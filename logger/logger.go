package logger

import (
	"fmt"
	"github.com/fatih/color"
	"strings"
	"sync"
	"time"
)

// Log levels constants
const (
	WARN  = 0
	DEBUG = iota
	INFO
	ERROR
)

// Logger struct to hold the current log level
type Logger struct {
	level int
}

// Global logger instance (accessible across the app)
var (
	loggerInstance *Logger
	once           sync.Once
)

// New creates or returns the global logger instance with the specified log level
// It only initializes the logger once
func New(level int) *Logger {
	once.Do(func() {
		loggerInstance = &Logger{level: level}
		fmt.Printf("Logger initialized with level: %d\n", level)
	})
	return loggerInstance
}

// log is an internal function to print messages with a specific log level and color
func (l *Logger) log(level int, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	var colorFunc func(...interface{}) string
	var levelStr string

	switch level {
	case WARN:
		colorFunc = color.New(color.FgYellow).Sprint
		levelStr = "[WARN]"
	case INFO:
		colorFunc = color.New(color.FgGreen).Sprint
		levelStr = "[INFO]"
	case DEBUG:
		colorFunc = color.New(color.FgBlue).Sprint
		levelStr = "[DEBUG]"
	case ERROR:
		colorFunc = color.New(color.FgRed).Sprint
		levelStr = "[ERROR]"
	default:
		colorFunc = color.New(color.FgWhite).Sprint
		levelStr = "[UNKNOWN]"
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s %s %s\n", colorFunc(timestamp), colorFunc(levelStr), msg)
}

// InitializeLogger creates a global logger instance based on the provided log level.
// This is an internal method, so it uses a lowercase name.
func InitializeLogger(level string) {
	if level == "" {
		level = "INFO"
	}
	level = strings.ToUpper(level)

	var logLevel int
	switch level {
	case "WARN":
		logLevel = WARN
	case "INFO":
		logLevel = INFO
	case "DEBUG":
		logLevel = DEBUG
	case "ERROR":
		logLevel = ERROR
	default:
		logLevel = INFO
	}

	loggerInstance = New(logLevel)
	loggerInstance.level = logLevel
	loggerInstance.log(INFO, "Initialized logger with level: %d\n", logLevel)
}

// SetDefaultLogger initializes the global logger with the default log level "WARN".
// This is typically used when no specific log level is provided by the application configuration.
func SetDefaultLogger() {
	InitializeLogger("WARN")
}

// Warn logs an info level message
func Warn(format string, args ...interface{}) {
	if loggerInstance == nil {
		return
	}
	loggerInstance.log(WARN, format, args...)
}

// Info logs an info level message
func Info(format string, args ...interface{}) {
	if loggerInstance == nil {
		return
	}
	loggerInstance.log(INFO, format, args...)
}

// Debug logs a debug level message
func Debug(format string, args ...interface{}) {
	if loggerInstance == nil {
		return
	}
	loggerInstance.log(DEBUG, format, args...)
}

// Error logs an error level message
func Error(format string, args ...interface{}) {
	if loggerInstance == nil {
		return
	}
	loggerInstance.log(ERROR, format, args...)
}
