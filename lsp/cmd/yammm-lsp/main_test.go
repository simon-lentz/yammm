package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_VersionFlag(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := run([]string{"--version"})

	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Errorf("run(--version) returned error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "yammm-lsp") {
		t.Errorf("version output missing 'yammm-lsp': %q", output)
	}
}

func TestRun_HelpFlag(t *testing.T) {
	err := run([]string{"-help"})
	if err != nil {
		t.Errorf("run(-help) returned error: %v", err)
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	err := run([]string{"--invalid-flag-xyz"})
	if err == nil {
		t.Error("run(--invalid-flag-xyz) should return an error")
	}
}

func TestRun_InvalidLogLevel(t *testing.T) {
	err := run([]string{"--log-level", "invalid"})
	if err == nil {
		t.Error("run(--log-level invalid) should return an error")
	}
	if !strings.Contains(err.Error(), "invalid log level") {
		t.Errorf("error should mention 'invalid log level': %v", err)
	}
}

func TestSetupLogger_ValidLevels(t *testing.T) {
	levels := []string{"error", "warn", "info", "debug", "trace"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			logger, cleanup, err := setupLogger(level, "")
			if err != nil {
				t.Errorf("setupLogger(%q, \"\") returned error: %v", level, err)
				return
			}
			if logger == nil {
				t.Errorf("setupLogger(%q, \"\") returned nil logger", level)
			}
			if cleanup == nil {
				t.Errorf("setupLogger(%q, \"\") returned nil cleanup", level)
			}
			cleanup()
		})
	}
}

func TestSetupLogger_InvalidLevel(t *testing.T) {
	_, _, err := setupLogger("invalid", "")
	if err == nil {
		t.Error("setupLogger(\"invalid\", \"\") should return an error")
	}
	if !strings.Contains(err.Error(), "invalid log level") {
		t.Errorf("error should mention 'invalid log level': %v", err)
	}
}

func TestSetupLogger_FileCreation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	logger, cleanup, err := setupLogger("info", logPath)
	if err != nil {
		t.Fatalf("setupLogger failed: %v", err)
	}

	if logger == nil {
		cleanup()
		t.Fatal("logger is nil")
	}

	// Write a log entry and close to flush before reading
	logger.Info("test message")
	cleanup()

	// Verify file was created and has content
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	if len(data) == 0 {
		t.Error("log file is empty")
	}
	if !strings.Contains(string(data), "test message") {
		t.Errorf("log file doesn't contain test message: %s", data)
	}
}

func TestSetupLogger_FileAppends(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Write initial content
	err := os.WriteFile(logPath, []byte("existing\n"), 0o600)
	if err != nil {
		t.Fatalf("failed to create initial log file: %v", err)
	}

	logger, cleanup, err := setupLogger("info", logPath)
	if err != nil {
		t.Fatalf("setupLogger failed: %v", err)
	}

	logger.Info("appended message")
	cleanup()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "existing") {
		t.Error("log file should preserve existing content")
	}
	if !strings.Contains(content, "appended message") {
		t.Error("log file should contain appended message")
	}
}

func TestFlagParsing_Defaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	logLevel := fs.String("log-level", "info", "")
	logFile := fs.String("log-file", "", "")
	moduleRoot := fs.String("module-root", "", "")
	showVer := fs.Bool("version", false, "")

	err := fs.Parse([]string{})
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if *logLevel != "info" {
		t.Errorf("default log-level: got %q, want %q", *logLevel, "info")
	}
	if *logFile != "" {
		t.Errorf("default log-file: got %q, want %q", *logFile, "")
	}
	if *moduleRoot != "" {
		t.Errorf("default module-root: got %q, want %q", *moduleRoot, "")
	}
	if *showVer {
		t.Error("default version: got true, want false")
	}
}

func TestFlagParsing_AllOptions(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	logLevel := fs.String("log-level", "info", "")
	logFile := fs.String("log-file", "", "")
	moduleRoot := fs.String("module-root", "", "")
	showVer := fs.Bool("version", false, "")

	err := fs.Parse([]string{
		"--log-level", "debug",
		"--log-file", "/tmp/test.log",
		"--module-root", "/path/to/root",
		"--version",
	})
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parse failed: %v", err)
	}

	if *logLevel != "debug" {
		t.Errorf("log-level: got %q, want %q", *logLevel, "debug")
	}
	if *logFile != "/tmp/test.log" {
		t.Errorf("log-file: got %q, want %q", *logFile, "/tmp/test.log")
	}
	if *moduleRoot != "/path/to/root" {
		t.Errorf("module-root: got %q, want %q", *moduleRoot, "/path/to/root")
	}
	if !*showVer {
		t.Error("version: got false, want true")
	}
}
