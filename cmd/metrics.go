package main

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
)

const metricsNamespace = "flux_reconciler"

var (
	clientsConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("%s_clients_connected", metricsNamespace),
		Help: "The current number of connected subscribers",
	})

	reconciledCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_reconciliations_total", metricsNamespace),
		Help: "The total number of reconciliations",
	}, []string{"name", "status", "namespace"})

	processedMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_processed_messages_total", metricsNamespace),
		Help: "The total number of processed messages",
	}, []string{"status"})

	connectionAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_connection_attempts_total", metricsNamespace),
		Help: "The total number of connection attempts",
	})

	webhooksHandled = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: fmt.Sprintf("%s_webhooks_handled_total", metricsNamespace),
		Help: "The total number of processed messages",
	}, []string{"status"})
)

func runMetricsServer(ctx context.Context, config Config, logger *zap.Logger) {
	setupMetrics(config)
	addr := fmt.Sprintf("%s:%s", config.Metrics.Host, config.Metrics.Port)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: addr, Handler: mux}
	go func() {
		logger.Info("Starting metrics server", zap.String("addr", addr))
		err := server.ListenAndServe()
		if err != nil {
			logger.Fatal("Failed to start server metrics", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down metrics server")

	if err := server.Shutdown(context.Background()); err != nil {
		logger.Fatal("Failed to shutdown metrics server", zap.Error(err))
	}
}

func setupMetrics(config Config) {
	prometheus.MustRegister(reconciledCount)
	if config.Mode == "server" { // server only metrics
		prometheus.MustRegister(clientsConnected)
		prometheus.MustRegister(webhooksHandled)
	} else { // client only metrics
		prometheus.MustRegister(processedMessages)
		prometheus.MustRegister(connectionAttempts)
	}
}
