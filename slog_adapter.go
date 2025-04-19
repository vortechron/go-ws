package ws

import (
	"fmt"
	"log/slog"
	"sync"
)

// SlogAdapter adapts the standard library's slog.Logger to our Logger interface
type SlogAdapter struct {
	logger *slog.Logger
	level  LogLevel
	mu     sync.RWMutex
}

// NewSlogAdapter creates a new SlogAdapter
func NewSlogAdapter(logger *slog.Logger, level LogLevel) *SlogAdapter {
	return &SlogAdapter{
		logger: logger,
		level:  level,
	}
}

// SetLevel sets the minimum log level
func (s *SlogAdapter) SetLevel(level LogLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.level = level
}

// GetLevel returns the current log level
func (s *SlogAdapter) GetLevel() LogLevel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.level
}

// Debug logs a debug message
func (s *SlogAdapter) Debug(format string, v ...interface{}) {
	if s.GetLevel() <= LevelDebug {
		s.logger.Debug(fmt.Sprintf(format, v...))
	}
}

// Info logs an info message
func (s *SlogAdapter) Info(format string, v ...interface{}) {
	if s.GetLevel() <= LevelInfo {
		s.logger.Info(fmt.Sprintf(format, v...))
	}
}

// Warn logs a warning message
func (s *SlogAdapter) Warn(format string, v ...interface{}) {
	if s.GetLevel() <= LevelWarn {
		s.logger.Warn(fmt.Sprintf(format, v...))
	}
}

// Error logs an error message
func (s *SlogAdapter) Error(format string, v ...interface{}) {
	if s.GetLevel() <= LevelError {
		s.logger.Error(fmt.Sprintf(format, v...))
	}
}

// Convert between our log levels and slog levels
func convertLogLevel(level LogLevel) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
