//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// stopSignals are the signals that stop a run. QUIT is among them because its
// default, a stack dump and an exit, would leave the worker trees and every
// running go command behind.
func stopSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
}

func signalNumber(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return int(s)
	}
	return 1
}

// ownGroup makes cmd the leader of a new process group, so one signal reaches
// every process it starts that stays in the group.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopGroup(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) //nolint:wrapcheck // os/exec reports it
}

// killGroup kills what is left of cmd's group. The group has no member when
// the command took everything it started with it.
func killGroup(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck // an empty group is the usual case
}

// groupAlive reports whether cmd's group still has a member.
func groupAlive(cmd *exec.Cmd) bool {
	return syscall.Kill(-cmd.Process.Pid, 0) == nil
}

// dumpStacks asks a Go program for its goroutines' stacks, which it prints
// before it exits.
func dumpStacks(p *os.Process) error {
	return p.Signal(syscall.SIGQUIT) //nolint:wrapcheck // the caller falls back to Kill
}
