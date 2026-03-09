package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevelInvalid(t *testing.T) {
	_, err := ParseLevel("verbose")
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestSetupInvalidFormat(t *testing.T) {
	_, err := Setup("info", "xml", "stdout")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestJSONHandlerOutputsExpectedFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	logger.Info("hello", slog.String("component", "proxy"), slog.Int("status", 200))

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}

	if got["msg"] != "hello" {
		t.Fatalf("expected msg=hello, got %v", got["msg"])
	}
	if got["level"] != "INFO" {
		t.Fatalf("expected level=INFO, got %v", got["level"])
	}
	if got["component"] != "proxy" {
		t.Fatalf("expected component=proxy, got %v", got["component"])
	}
	if got["status"] != float64(200) {
		t.Fatalf("expected status=200, got %v", got["status"])
	}
	if _, ok := got["time"]; !ok {
		t.Fatal("expected time field in JSON output")
	}
}

func TestTextHandlerOutputsText(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	logger.Info("hello", slog.String("component", "proxy"))

	output := buf.String()
	if !strings.Contains(output, "level=INFO") {
		t.Fatalf("expected text output to contain level, got %q", output)
	}
	if !strings.Contains(output, "msg=hello") {
		t.Fatalf("expected text output to contain message, got %q", output)
	}
	if !strings.Contains(output, "component=proxy") {
		t.Fatalf("expected text output to contain component, got %q", output)
	}
}

func TestLevelFilteringHidesDebugAtInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	logger.Debug("debug hidden")
	logger.Info("info shown")

	output := buf.String()
	if strings.Contains(output, "debug hidden") {
		t.Fatalf("expected debug message to be filtered, got %q", output)
	}
	if !strings.Contains(output, "info shown") {
		t.Fatalf("expected info message to be logged, got %q", output)
	}
}

func TestLogAccessProducesStructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	LogAccess(logger, AccessLogEntry{
		SourceIP:   "192.168.1.100",
		Method:     "CONNECT",
		Domain:     "api.github.com:443",
		Status:     200,
		BytesIn:    0,
		BytesOut:   15234,
		DurationMs: 45,
		Allowed:    true,
		Reason:     "ok",
	})

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("expected valid JSON output: %v", err)
	}

	if got["msg"] != "access" {
		t.Fatalf("expected msg=access, got %v", got["msg"])
	}

	proxy, ok := got["proxy"].(map[string]any)
	if !ok {
		t.Fatalf("expected proxy group object, got %T", got["proxy"])
	}

	assertEqual := func(key string, want any) {
		t.Helper()
		if proxy[key] != want {
			t.Fatalf("expected proxy.%s=%v, got %v", key, want, proxy[key])
		}
	}

	assertEqual("source_ip", "192.168.1.100")
	assertEqual("method", "CONNECT")
	assertEqual("domain", "api.github.com:443")
	assertEqual("status", float64(200))
	assertEqual("bytes_in", float64(0))
	assertEqual("bytes_out", float64(15234))
	assertEqual("duration_ms", float64(45))
	assertEqual("allowed", true)
	assertEqual("reason", "ok")
}
