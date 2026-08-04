package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	incomingPackets prometheus.CounterVec
	bouncePackets   prometheus.CounterVec
}

func initMetrics() (*prometheus.Registry, *metrics) {
	reg := prometheus.NewRegistry()

	m := &metrics{
		incomingPackets: *promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "apx_incoming_packets_total",
				Help: "Total number of incoming packets per slot and command type",
			},
			[]string{"slot", "cmd"},
		),
		bouncePackets: *promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "apx_bounce_packets_total",
				Help: "Total number of bounce packets per slot and tag",
			},
			[]string{"slot", "tag"},
		),
	}

	return reg, m
}
