package main

import (
	"encoding/json"
	"fmt"
	"log"
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

	// Try to send to channel non-blocking
	select {
	case l.logChannel <- logMsg:
		// Successfully sent to channel
	default:
		// Channel is full, drop oldest message and add new one
		select {
		case <-l.logChannel:
			// Removed oldest message
		default:
			// Channel was empty after all
		}
		// Try to send again
		select {
		case l.logChannel <- logMsg:
			// Successfully sent after making space
		default:
			// Still full, log to console as fallback
			log.Printf("[%s] Log channel full, dropped message: %s", level, message)
		}
	}

	// Always log errors to console as well
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
