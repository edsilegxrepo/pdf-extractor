//go:build windows

// process_windows.go provides Windows-specific process group management.
//
// Purpose:
//   Enables clean termination of mutool subprocesses on Windows when timeouts
//   occur or errors are encountered. Without process group isolation, child
//   processes could be orphaned if the parent is killed.
//
// Windows Implementation:
//   Uses CREATE_NEW_PROCESS_GROUP flag to place each mutool invocation in its
//   own process group. This allows the entire process tree to be terminated
//   together via the standard Kill() method.
//
// Note: On Windows, TerminateProcess (used by cmd.Process.Kill) terminates
// the target process but not necessarily its children. The process group flag
// helps by isolating the subprocess tree, and Go's CommandContext handles
// the primary termination. The killProcessGroup function provides best-effort
// cleanup for any remaining processes.

package main

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to create a new process group on Windows.
// This isolates the mutool process tree from the main application process group,
// enabling clean termination of mutool and any child processes it may spawn.
//
// Called by processFile() before executing mutool.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessGroup terminates the mutool process on Windows.
// Called when mutool times out or encounters an error to ensure no orphaned processes.
//
// On Windows, cmd.Process.Kill() calls TerminateProcess which forcefully ends
// the process. Combined with process group isolation from setupProcessGroup,
// this provides reasonable cleanup of the subprocess tree.
//
// Error from Kill() is intentionally ignored:
//   - Process may have already exited normally
//   - Best-effort cleanup; failure is not fatal to the application
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill() // Best-effort termination; error expected if already dead
	}
}
