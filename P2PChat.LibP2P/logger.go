package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Logger handles all logging operations
type Logger struct {
	logChannel chan LogMessage
}

// NewLogger creates a new logger instance
func NewLogger() *Logger {
	return &Logger{
		logChannel: make(chan LogMessage, 100), // Buffer for logs
	}
}

// LogToFrontend sends a log message to the frontend
func (l *Logger) LogToFrontend(level string, format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	logMsg := LogMessage{Level: level, Message: message}

	// Try to send to channel with timeout
	select {
	case l.logChannel <- logMsg:
		// Successfully sent to channel
	case <-time.After(1 * time.Second):
		// Channel is full or blocked, log to console
		log.Printf("[%s] Failed to send log to channel: %s", level, message)
	}

	// Only log to console if it's an error
	if level == "ERROR" {
		log.Printf("[%s] %s", level, message)
	}
}

// GetLogChannel returns the log channel for reading
func (l *Logger) GetLogChannel() <-chan LogMessage {
	return l.logChannel
}

// MarshalLogMessage marshals a log message to JSON
func (l *Logger) MarshalLogMessage(logMsg LogMessage) ([]byte, error) {
	return json.Marshal(logMsg)
}
