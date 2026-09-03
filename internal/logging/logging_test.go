package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

func TestNewWritesJSONFileAtConfiguredLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "hedging-bot.log")
	bundle, err := New(Config{
		Output:  OutputFile,
		File:    path,
		Level:   "info",
		Format:  FormatJSON,
		Service: "test-service",
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle.Logger.Debug().Msg("hidden")
	bundle.Logger.Info().Str("symbol", "BNBUSDT").Msg("visible")
	bundle.AsynqLogger.Info("asynq visible")
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "hidden") {
		t.Fatalf("debug log must be filtered: %s", text)
	}
	if !strings.Contains(text, `"message":"visible"`) || !strings.Contains(text, `"service":"test-service"`) {
		t.Fatalf("unexpected JSON log: %s", text)
	}
	if !strings.Contains(text, `"component":"asynq"`) || !strings.Contains(text, `"message":"asynq visible"`) {
		t.Fatalf("asynq log did not use configured output: %s", text)
	}
}

func TestNewMapsAsynqLogLevel(t *testing.T) {
	bundle, err := New(Config{Output: OutputConsole, Level: "warn", Format: FormatJSON})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
	if bundle.AsynqLogLevel != asynq.WarnLevel {
		t.Fatalf("expected asynq warn level, got %v", bundle.AsynqLogLevel)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	tests := []Config{
		{Output: "unknown", Level: "info", Format: FormatJSON},
		{Output: OutputFile, Level: "info", Format: FormatJSON},
		{Output: OutputConsole, Level: "verbose", Format: FormatJSON},
		{Output: OutputConsole, Level: "info", Format: "xml"},
	}

	for _, config := range tests {
		if bundle, err := New(config); err == nil {
			_ = bundle.Close()
			t.Fatalf("expected invalid config to fail: %+v", config)
		}
	}
}
