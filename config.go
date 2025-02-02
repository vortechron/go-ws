package ws

import "time"

type Config struct {
	PingInterval        time.Duration
	WriteBufferSize     int
	ReadBufferSize      int
	RateLimit           RateLimit
	MaxConnections      int
	MaxConnectionsPerIP int
}

type RateLimit struct {
	Messages int
	Interval time.Duration
}
