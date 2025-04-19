package ws

import "time"

type Config struct {
	PingInterval        time.Duration
	WriteBufferSize     int
	ReadBufferSize      int
	RateLimit           RateLimit
	MaxConnections      int
	MaxConnectionsPerIP int
	ShouldLogStats      bool
	LogLevel            LogLevel
}

type RateLimit struct {
	Messages int
	Interval time.Duration
}

// NewDefaultConfig creates a new config with default values
func NewDefaultConfig() Config {
	return Config{
		PingInterval:        30 * time.Second,
		WriteBufferSize:     1024,
		ReadBufferSize:      1024,
		MaxConnections:      1000,
		MaxConnectionsPerIP: 100,
		ShouldLogStats:      true,
		LogLevel:            LevelInfo,
		RateLimit: RateLimit{
			Messages: 100,
			Interval: time.Minute,
		},
	}
}
