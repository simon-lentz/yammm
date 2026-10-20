//go:build !unix

package main

import (
	"os"
	"os/exec"
)

// stopSignals are the signals that stop a run.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func signalNumber(os.Signal) int { return 2 }

// ownGroup does nothing: this host has no process group to signal, and a stop
// reaches the go command alone.
func ownGroup(*exec.Cmd) {}

func stopGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill() //nolint:wrapcheck // os/exec reports it
}

func killGroup(*exec.Cmd) {}

func groupAlive(*exec.Cmd) bool { return false }

// dumpStacks kills p: this host has no signal that asks for stacks.
func dumpStacks(p *os.Process) error {
	return p.Kill() //nolint:wrapcheck // the caller falls back to Kill
}
