package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	clientsConnected = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "server_connected_clients",
		Help: "The total number of connected subscribers",
	})
)

func init() {
	prometheus.MustRegister(clientsConnected)
}
