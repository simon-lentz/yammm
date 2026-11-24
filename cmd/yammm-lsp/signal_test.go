//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestMain runs the server when the test re-executes this binary as one.
func TestMain(m *testing.M) {
	if os.Getenv(serveEnv) != "" {
		os.Args = os.Args[:1]
		main()
		return
	}
	os.Exit(m.Run())
}

const serveEnv = "YAMMM_LSP_TEST_SERVE"

// A client that stops reading fills the output pipe, and the server's write then holds
// the lock its Stop takes. A signal must still end the process at the bound, and the
// bound's own length is what in-flight handlers get.
func TestRun_ASignalEndsAServerBlockedOnItsOutput(t *testing.T) {
	t.Parallel()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outR.Close()
	defer outW.Close()

	cmd := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // runs this test binary as the server
	cmd.Env = append(os.Environ(), serveEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()

	// Each request draws an error response, and nothing reads them. The writer stops
	// when the server stops reading, so it runs apart from the test.
	go func() {
		for i := range 100000 {
			body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"no/such/method"}`, i)
			if _, err := fmt.Fprintf(inW, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
				return
			}
		}
	}()
	time.Sleep(time.Second)

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the process exited with %v, want 0:\n%s", err, stderr.String())
		}
		if took := time.Since(start); took < 5*time.Second {
			t.Errorf("the process exited after %v, want the whole five-second bound for its handlers", took)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the process outlived the signal's bound")
	}
}

// A blocking stdin's read outlives every call, and the process must not wait for it.
func TestRun_ASignalEndsABlockingReadAtOnce(t *testing.T) {
	t.Parallel()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outR.Close()
	defer outW.Close()

	cmd := exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec // runs this test binary as the server
	cmd.Env = append(os.Environ(), serveEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()

	// A response proves the server's read is running.
	body := `{"jsonrpc":"2.0","id":1,"method":"no/such/method"}`
	if _, err := fmt.Fprintf(inW, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}
	if _, err := outR.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the process exited with %v, want 0:\n%s", err, stderr.String())
		}
		if took := time.Since(start); took >= 5*time.Second {
			t.Errorf("the process took %v to exit, want it inside the five-second bound", took)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the process outlived the signal")
	}
}
