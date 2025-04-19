package ws

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// LogLevel represents logging severity
type LogLevel int

const (
	// Log levels
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[LogLevel]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

// Logger is an interface that allows for flexible logging implementations
type Logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

// StandardLogger implements Logger using the standard log package
type StandardLogger struct {
	logger *log.Logger
	level  LogLevel
	mu     sync.RWMutex
}

// NewLogger creates a new StandardLogger with the specified level
func NewLogger(level LogLevel, output io.Writer) *StandardLogger {
	if output == nil {
		output = os.Stderr
	}
	return &StandardLogger{
		logger: log.New(output, "", log.LstdFlags),
		level:  level,
	}
}

// SetLevel sets the minimum log level
func (l *StandardLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *StandardLogger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// Debug logs a debug message
func (l *StandardLogger) Debug(format string, v ...interface{}) {
	if l.GetLevel() <= LevelDebug {
		l.log(LevelDebug, format, v...)
	}
}

// Info logs an info message
func (l *StandardLogger) Info(format string, v ...interface{}) {
	if l.GetLevel() <= LevelInfo {
		l.log(LevelInfo, format, v...)
	}
}

// Warn logs a warning message
func (l *StandardLogger) Warn(format string, v ...interface{}) {
	if l.GetLevel() <= LevelWarn {
		l.log(LevelWarn, format, v...)
	}
}

// Error logs an error message
func (l *StandardLogger) Error(format string, v ...interface{}) {
	if l.GetLevel() <= LevelError {
		l.log(LevelError, format, v...)
	}
}

// log logs a message with the specified level
func (l *StandardLogger) log(level LogLevel, format string, v ...interface{}) {
	prefix := fmt.Sprintf("[%s] ", levelNames[level])
	msg := fmt.Sprintf(format, v...)
	l.logger.Println(prefix + msg)
}

// Global default logger
var defaultLogger = NewLogger(LevelInfo, nil)

// Default returns the default logger
func DefaultLogger() Logger {
	return defaultLogger
}

// SetDefaultLogger sets the default logger
func SetDefaultLogger(logger Logger) {
	if stdLogger, ok := logger.(*StandardLogger); ok {
		defaultLogger = stdLogger
	}
}

// Debug logs a debug message using the default logger
func Debug(format string, v ...interface{}) {
	defaultLogger.Debug(format, v...)
}

// Info logs an info message using the default logger
func Info(format string, v ...interface{}) {
	defaultLogger.Info(format, v...)
}

// Warn logs a warning message using the default logger
func Warn(format string, v ...interface{}) {
	defaultLogger.Warn(format, v...)
}

// Error logs an error message using the default logger
func Error(format string, v ...interface{}) {
	defaultLogger.Error(format, v...)
}

// SetLogLevel sets the level of the default logger
func SetLogLevel(level LogLevel) {
	defaultLogger.SetLevel(level)
}

// GetLogLevel gets the level of the default logger
func GetLogLevel() LogLevel {
	return defaultLogger.GetLevel()
}
