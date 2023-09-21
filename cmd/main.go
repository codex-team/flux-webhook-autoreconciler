package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"net/url"
	"time"
)

func setupServer(config Config, logger *zap.Logger) {
	reconciler := NewReconciler(logger)
	handlers := NewHandlers(config, reconciler, logger)
	http.Handle("/webhook", WithLogging(http.HandlerFunc(handlers.Webhook), logger))
	http.Handle("/subscribe", WithLogging(http.HandlerFunc(handlers.Subscribe), logger))
}

func setupClient(config Config, shutdownChan chan struct{}, logger *zap.Logger) {
	logger.Info("Starting client")
	reconciler := NewReconciler(logger)

	u, err := url.Parse(config.ServerEndpoint)
	if err != nil {
		logger.Fatal("Failed to parse server endpoint", zap.Error(err))
	}

	query := u.Query()
	query.Set("authSecret", config.SubscribeSecret)
	u.RawQuery = query.Encode()

	// Number of retry attempts
	maxRetries := 5

	// Time delay between retries
	retryDelay := time.Second * 5

	retry := 0

	go func() {
		for retry < maxRetries {
			logger.Info("Connecting to server", zap.String("endpoint", config.ServerEndpoint))

			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				time.Sleep(retryDelay)
				retry++
				logger.Error("Failed to connect to server", zap.Error(err), zap.Int("retry", retry))
				continue
			}
			defer c.Close()
			logger.Info("Connected to server", zap.String("endpoint", config.ServerEndpoint))

			retry = 0

			for {
				messageType, message, err := c.ReadMessage()
				if err != nil {
					logger.Error("Error reading message", zap.Error(err))
					break
				}

				if messageType != websocket.BinaryMessage {
					logger.Info("Received non-binary message", zap.String("message", string(message)))
					continue
				}

				var payload SubscribeEventPayload
				err = json.Unmarshal(message, &payload)
				if err != nil {
					logger.Error("Error unmarshalling message", zap.Error(err))
					break
				}

				reconciler.ReconcileSources(payload.OciUrl, payload.Tag)
			}

		}
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
	flag.StringVar(&loggerMode, "logger", "prod", "Logger mode")

	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to config file")
	flag.Parse()

	var logger *zap.Logger
	if loggerMode == "dev" {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}

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
