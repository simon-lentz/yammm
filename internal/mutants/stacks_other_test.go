//go:build !unix

package main

// ignoreStackRequest does nothing: this host stops a process by killing it.
func ignoreStackRequest() {}
