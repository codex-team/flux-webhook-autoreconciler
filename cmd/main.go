package main

import (
	"context"
	"flag"
	"fmt"
	"go.uber.org/zap"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func runServer(ctx context.Context, wg *sync.WaitGroup, config Config, logger *zap.Logger) {
	defer wg.Done()
	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)

	k8sClient, err := getRestClient()
	if err != nil {
		logger.Fatal("Failed to get Kubernetes client", zap.Error(err))
	}
	mux := http.NewServeMux()
	reconciler := NewReconciler(k8sClient, logger)
	handlers := NewHandlers(config, reconciler, logger)
	mux.Handle("/webhook", WithLogging(http.HandlerFunc(handlers.Webhook), logger))
	mux.Handle("/subscribe", WithLogging(http.HandlerFunc(handlers.Subscribe), logger))
	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		logger.Info("Starting server", zap.String("addr", addr))
		err := server.ListenAndServe()
		if err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down server")

	if err := server.Shutdown(context.Background()); err != nil {
		logger.Fatal("Failed to shutdown server", zap.Error(err))
	}
}

func runClient(ctx context.Context, wg *sync.WaitGroup, config Config, logger *zap.Logger) {
	defer wg.Done()
	k8sClient, err := getRestClient()
	if err != nil {
		logger.Fatal("Failed to get Kubernetes client", zap.Error(err))
	}
	reconciler := NewReconciler(k8sClient, logger)

	u, err := url.Parse(config.ServerEndpoint)
	if err != nil {
		logger.Fatal("Failed to parse server endpoint", zap.Error(err))
	}

	query := u.Query()
	query.Set("authSecret", config.SubscribeSecret)
	u.RawQuery = query.Encode()

	client := NewClient(u, reconciler, logger)

	client.Run(ctx)
}

func WithLogging(h http.Handler, logger *zap.Logger) http.Handler {
	logFn := func(rw http.ResponseWriter, r *http.Request) {
		logger.Info("Handle incoming request", zap.String("method", r.Method), zap.String("path", r.URL.Path))

		h.ServeHTTP(rw, r)

		logger.Info("Finished handling request", zap.String("method", r.Method), zap.String("path", r.URL.Path))
	}
	return http.HandlerFunc(logFn)
}

func main() {
	var loggerMode string
	flag.StringVar(&loggerMode, "log-mode", "prod", "Logger mode")

	var logLevel string
	flag.StringVar(&logLevel, "log-level", "info", "Log level")

	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to config file")
	flag.Parse()

	var loggerConfig zap.Config
	if loggerMode == "dev" {
		loggerConfig = zap.NewDevelopmentConfig()
	} else {
		loggerConfig = zap.NewProductionConfig()
	}

	level, err := zap.ParseAtomicLevel(logLevel)

	if err != nil {
		logger := zap.Must(zap.NewProduction())
		logger.Fatal("Failed to parse log level", zap.Error(err))
	}

	loggerConfig.Level = level
	logger := zap.Must(loggerConfig.Build())
	defer logger.Sync()

	config, err := LoadConfig(configPath)

	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	wg.Add(1)
	if config.Mode == "server" {
		go runServer(ctx, wg, config, logger)
	} else {
		go runClient(ctx, wg, config, logger)
	}

	if config.Metrics.Enabled {
		go runMetricsServer(ctx, config, logger)
	}

	go func() {
		<-signalCh
		logger.Info("Received signal, shutting down")
		cancel()
	}()

	wg.Wait()
}
