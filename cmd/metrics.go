package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
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
	}, []string{"source", "status"})

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
