package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"net/url"
)

func setupServer(config Config, logger *zap.Logger) {
	reconciler := NewReconciler(logger)
	handlers := NewHandlers(config, reconciler, logger)
	http.Handle("/webhook", WithLogging(http.HandlerFunc(handlers.Webhook), logger))
	http.Handle("/subscribe", WithLogging(http.HandlerFunc(handlers.Subscribe), logger))
}

func setupClient(config Config, shutdownChan chan struct{}, logger *zap.Logger) {
	reconciler := NewReconciler(logger)

	u, err := url.Parse(config.ServerEndpoint)
	if err != nil {
		logger.Fatal("Failed to parse server endpoint", zap.Error(err))
	}

	query := u.Query()
	query.Set("authSecret", config.SubscribeSecret)
	u.RawQuery = query.Encode()

	client := NewClient(u, reconciler, logger)

	go func() {
		client.Run()
		shutdownChan <- struct{}{}
	}()
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

	http.Handle("/metrics", promhttp.Handler())

	shutdownChan := make(chan struct{})

	if config.Mode == "server" {
		setupServer(config, logger)
	} else {
		setupClient(config, shutdownChan, logger)
	}

	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)

	server := &http.Server{Addr: addr}

	go func() {
		logger.Info("Starting server", zap.String("addr", addr))
		err := server.ListenAndServe()
		if err != nil {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()
	select {
	case <-shutdownChan:
		logger.Info("Shutting down server")
		// Received a shutdown signal, so shut down the server gracefully
		if err := server.Shutdown(context.Background()); err != nil {
			logger.Fatal("Failed to shutdown server", zap.Error(err))
		}
	}
}
