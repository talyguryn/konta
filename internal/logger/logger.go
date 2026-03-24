package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	logFile     *os.File
	logDir      string
	logFilePath string
	currentLevel = "info"
)

var levelPriority = map[string]int{
	"debug": 0,
	"info":  1,
	"warn":  2,
	"error": 3,
	"fatal": 4,
}

// SetLevel configures the minimal log level that should be emitted.
func SetLevel(level string) {
	normalized := normalizeLevel(level)
	if _, ok := levelPriority[normalized]; ok {
		currentLevel = normalized
	}
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error", "fatal":
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return "info"
	}
}

func shouldLog(level string) bool {
	msgLevel, ok := levelPriority[strings.ToLower(level)]
	if !ok {
		msgLevel = levelPriority["info"]
	}
	current, ok := levelPriority[currentLevel]
	if !ok {
		current = levelPriority["info"]
	}
	return msgLevel >= current
}

// Init initializes the logger
func Init(customLogPath string) error {
	if customLogPath != "" {
		logFilePath = customLogPath
		logDir = filepath.Dir(logFilePath)

		// Create log directory if it doesn't exist
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}

		var err error
		logFile, err = os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to open log file: %w", err)
		}

		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		homeDir = "/tmp"
	}

	candidates := []string{
		"/var/log/konta/konta.log",
		filepath.Join(homeDir, ".konta", "logs", "konta.log"),
	}

	var lastErr error
	for _, candidate := range candidates {
		if err := os.MkdirAll(filepath.Dir(candidate), 0755); err != nil {
			lastErr = err
			continue
		}

		logFile, err = os.OpenFile(candidate, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			lastErr = err
			continue
		}

		logFilePath = candidate
		logDir = filepath.Dir(candidate)
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown error")
	}

	return fmt.Errorf("failed to open log file: %w", lastErr)
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
	_ = Close()
	os.Exit(1)
}

// Debug logs a debug message
func Debug(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logMessage("DEBUG", msg)
}

func logMessage(level, message string) {
	if !shouldLog(level) {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedMsg := fmt.Sprintf("[%s] [%s] %s", timestamp, level, message)

	fmt.Println(formattedMsg)

	if logFile != nil {
		log.SetOutput(logFile)
		log.Println(formattedMsg)
	}
}
