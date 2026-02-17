// Package main provides the entry point for the yammm-lsp language server.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/simon-lentz/yammm/lsp"
)

var version = "dev"

// LevelTrace is a custom log level below debug for verbose tracing.
const LevelTrace = slog.Level(-8)

// isCleanShutdown checks if an error represents a normal client disconnect.
// LSP clients commonly close stdio on exit, which should not be reported as fatal.
func isCleanShutdown(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, os.ErrClosed) {
		return true
	}
	// Check for broken pipe errors, which occur when the client closes
	// its end of the connection. This is portable across platforms.
	errStr := err.Error()
	if strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "EPIPE") {
		return true
	}
	return false
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "yammm-lsp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("yammm-lsp", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // Suppress default output; we print usage ourselves

	var (
		logLevel   = fs.String("log-level", "info", "log level: error|warn|info|debug|trace")
		logFile    = fs.String("log-file", "", "log file path (empty to log to stderr)")
		moduleRoot = fs.String("module-root", "", "override module root for import resolution")
		showVer    = fs.Bool("version", false, "print version and exit")
		_          = fs.Bool("stdio", false, "use stdio transport (default, accepted for VS Code compatibility)")
	)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: yammm-lsp [options]\n\n")
		fmt.Fprintf(os.Stderr, "YAMMM Language Server Protocol implementation.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		// Temporarily set output to stderr for PrintDefaults. The flagset output
		// is set to Discard above to suppress default flag error messages, but
		// we want usage/help to be visible when explicitly requested.
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -help was requested, usage already printed
		}
		fs.Usage()
		return fmt.Errorf("parse flags: %w", err)
	}

	if *showVer {
		fmt.Printf("yammm-lsp %s\n", version)
		return nil
	}

	// Set up logging
	logger, cleanup, err := setupLogger(*logLevel, *logFile)
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer cleanup()

	logger.Info("starting yammm-lsp",
		slog.String("version", version),
		slog.String("log_level", *logLevel),
	)

	// Canonicalize module root to match how document paths are resolved.
	// This ensures consistent path comparisons, especially on macOS where
	// /var symlinks to /private/var.
	canonicalModuleRoot := *moduleRoot
	if canonicalModuleRoot != "" {
		if absRoot, err := filepath.Abs(canonicalModuleRoot); err == nil {
			if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
				absRoot = resolved
			}
			canonicalModuleRoot = filepath.Clean(absRoot)
			if canonicalModuleRoot != *moduleRoot {
				logger.Debug("canonicalized module root",
					slog.String("original", *moduleRoot),
					slog.String("canonical", canonicalModuleRoot),
				)
			}
		}

		// Validate that module root exists and is a directory.
		// We warn rather than fail because the path might be created later
		// and the server has fallback mechanisms for import resolution.
		if info, err := os.Stat(canonicalModuleRoot); err != nil {
			logger.Warn("module root does not exist; import resolution may fail",
				slog.String("path", canonicalModuleRoot),
				slog.String("error", err.Error()),
			)
		} else if !info.IsDir() {
			logger.Warn("module root is not a directory; import resolution may fail",
				slog.String("path", canonicalModuleRoot),
			)
		}
	}

	// Create and configure server
	cfg := lsp.Config{
		ModuleRoot: canonicalModuleRoot,
	}

	server := lsp.NewServer(logger, cfg)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Run the server in a goroutine so we can select on signals
	errCh := make(chan error, 1)
	go func() { errCh <- server.RunStdio() }()

	logger.Info("running on stdio")

	select {
	case err := <-errCh:
		if err != nil {
			if isCleanShutdown(err) {
				logger.Debug("client closed connection")
			} else {
				return fmt.Errorf("run server: %w", err)
			}
		}
		logger.Info("server shutdown complete")
		return nil
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", slog.String("signal", sig.String()))
		server.Shutdown()
		if err := server.Close(); err != nil {
			logger.Warn("error closing connection", slog.String("error", err.Error()))
		}

		// Close stdin to unblock RunStdio's read operation.
		// When running manually (not connected to an LSP client), the JSON-RPC
		// connection's Close() doesn't close the underlying stdin, leaving
		// RunStdio blocked on os.Stdin.Read().
		if err := os.Stdin.Close(); err != nil {
			logger.Debug("error closing stdin", slog.String("error", err.Error()))
		}

		// Bounded wait for RunStdio to return. This prevents hanging forever
		// if Close() was called before the connection was initialized, or if
		// RunStdio() doesn't return for some other reason.
		select {
		case err := <-errCh:
			if err != nil {
				// Close intentionally causes an error; log at debug level
				logger.Debug("RunStdio returned after close", slog.String("error", err.Error()))
			}
		case <-time.After(5 * time.Second):
			logger.Warn("shutdown timed out, forcing exit")
		}

		logger.Info("server shutdown complete")
		// Return nil for exit code 0: LSP clients interpret this as clean shutdown.
		return nil
	}
}

func setupLogger(level, logFile string) (*slog.Logger, func(), error) {
	var slogLevel slog.Level
	switch level {
	case "error":
		slogLevel = slog.LevelError
	case "warn":
		slogLevel = slog.LevelWarn
	case "info":
		slogLevel = slog.LevelInfo
	case "debug":
		slogLevel = slog.LevelDebug
	case "trace":
		slogLevel = LevelTrace
	default:
		return nil, nil, fmt.Errorf("invalid log level: %q", level)
	}

	var writers []io.Writer
	var cleanup func()

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		writers = append(writers, f)
		cleanup = func() { _ = f.Close() }
	} else {
		// Write to stderr when no log file specified
		writers = append(writers, os.Stderr)
		cleanup = func() {}
	}

	var w io.Writer
	if len(writers) == 1 {
		w = writers[0]
	} else {
		w = io.MultiWriter(writers...)
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: true,
	})

	return slog.New(handler), cleanup, nil
}
