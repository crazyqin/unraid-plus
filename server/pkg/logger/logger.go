// Package logger is a tiny structured-ish logger backed by the standard
// library. It avoids pulling in zerolog/zap to keep the image small.
package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// Level is the log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var current Level = LevelInfo

// SetLevel updates the global minimum level from a string.
func SetLevel(s string) {
	switch strings.ToLower(s) {
	case "debug":
		current = LevelDebug
	case "warn", "warning":
		current = LevelWarn
	case "error":
		current = LevelError
	default:
		current = LevelInfo
	}
}

func logf(lvl Level, tag, format string, args ...any) {
	if lvl < current {
		return
	}
	msg := fmt.Sprintf(format, args...)
	log.Printf("[%s] %s | %s", strings.ToUpper(tag), time.Now().Format("15:04:05"), msg)
}

// Debugf logs at debug level.
func Debugf(format string, args ...any) { logf(LevelDebug, "debug", format, args...) }

// Infof logs at info level.
func Infof(format string, args ...any) { logf(LevelInfo, "info", format, args...) }

// Warnf logs at warn level.
func Warnf(format string, args ...any) { logf(LevelWarn, "warn", format, args...) }

// Errorf logs at error level.
func Errorf(format string, args ...any) { logf(LevelError, "error", format, args...) }

// Fatal logs at error level then exits with status 1.
func Fatal(format string, args ...any) {
	Errorf(format, args...)
	os.Exit(1)
}
