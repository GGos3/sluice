package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type AccessLogEntry struct {
	SourceIP   string
	Method     string
	Domain     string
	Status     int
	BytesIn    int64
	BytesOut   int64
	DurationMs int64
	Allowed    bool
	Reason     string
}

func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level: %s", level)
	}
}

func Setup(level string, format string, output string) (*slog.Logger, error) {
	parsedLevel, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}

	writer, err := resolveWriter(output)
	if err != nil {
		return nil, err
	}

	options := &slog.HandlerOptions{Level: parsedLevel}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(writer, options)
	case "text":
		handler = slog.NewTextHandler(writer, options)
	default:
		if closer, ok := writer.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("invalid log format: %s", format)
	}

	return slog.New(handler), nil
}

func resolveWriter(output string) (io.Writer, error) {
	switch strings.ToLower(output) {
	case "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	default:
		file, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		return file, nil
	}
}

func LogAccess(logger *slog.Logger, entry AccessLogEntry) {
	if logger == nil {
		return
	}

	logger.Info("access", slog.Group("proxy",
		slog.String("source_ip", entry.SourceIP),
		slog.String("method", entry.Method),
		slog.String("domain", entry.Domain),
		slog.Int("status", entry.Status),
		slog.Int64("bytes_in", entry.BytesIn),
		slog.Int64("bytes_out", entry.BytesOut),
		slog.Int64("duration_ms", entry.DurationMs),
		slog.Bool("allowed", entry.Allowed),
		slog.String("reason", entry.Reason),
	))
}
