package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	incomingPackets  prometheus.CounterVec
	bouncePackets    prometheus.CounterVec
	connectedClients prometheus.GaugeVec
}

func initMetrics() (*prometheus.Registry, *metrics) {
	reg := prometheus.NewRegistry()

	m := &metrics{
		incomingPackets: *promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "apx_incoming_packets_total",
				Help: "Total number of incoming packets per slot and command type",
			},
			[]string{"room", "slot", "game", "cmd"},
		),
		bouncePackets: *promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "apx_bounce_packets_total",
				Help: "Total number of bounce packets per slot and tag",
			},
			[]string{"room", "slot", "game", "tag"},
		),
		connectedClients: *promauto.With(reg).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "apx_connections",
				Help: "Number of connections to room",
			},
			[]string{"room"},
		),
	}

	return reg, m
}
