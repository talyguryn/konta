package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	logFile     *os.File
	logDir      string
	logFilePath string
)

// Init initializes the logger
func Init(customLogPath string) error {
	if customLogPath != "" {
		logFilePath = customLogPath
	} else {
		// Try to use /var/log/konta first
		logDir = "/var/log/konta"
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// Fallback to user home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				homeDir = "/tmp"
			}
			logDir = filepath.Join(homeDir, ".konta", "logs")
		}
		logFilePath = filepath.Join(logDir, "konta.log")
	}

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	var err error
	logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	return nil
}

// Close closes the log file
func Close() error {
	if logFile != nil {
		return logFile.Close()
	}
	return nil
}

// Info logs an info message
func Info(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("INFO", msg)
}

// Warn logs a warning message
func Warn(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("WARN", msg)
}

// Error logs an error message
func Error(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("ERROR", msg)
}

// Fatal logs an error message and exits
func Fatal(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("FATAL", msg)
	Close()
	os.Exit(1)
}

// Debug logs a debug message
func Debug(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("DEBUG", msg)
}

func logMessage(level, message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedMsg := fmt.Sprintf("[%s] [%s] %s", timestamp, level, message)

	fmt.Println(formattedMsg)

	if logFile != nil {
		log.SetOutput(logFile)
		log.Println(formattedMsg)
	}
}
