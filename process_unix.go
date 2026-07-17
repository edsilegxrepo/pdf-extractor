//go:build !windows

// process_unix.go provides Unix-specific process group management.
//
// Purpose:
//   Enables clean termination of mutool subprocesses on Unix systems (Linux, macOS)
//   when timeouts occur or errors are encountered. Without process group isolation,
//   child processes could be orphaned if the parent is killed.
//
// Unix Implementation:
//   Uses Setpgid to place each mutool invocation in its own process group.
//   On termination, sends SIGKILL to the negative PID (-pgid), which signals
//   all processes in the group simultaneously.
//
// Process Group Semantics:
//   - Setpgid=true: Child process becomes leader of a new process group
//   - Process group ID equals the child's PID
//   - kill(-pid, SIGKILL): Sends signal to all processes with pgid=pid
//   - SIGKILL: Uncatchable, immediate termination (no cleanup handlers run)
//
// Note: SIGKILL is used instead of SIGTERM because:
//   - mutool may be stuck on I/O or in an uninterruptible state
//   - Clean shutdown is not required for a subprocess doing stateless work
//   - Guarantees termination regardless of mutool's signal handling

package main

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to run in its own process group on Unix.
// This isolates the mutool process tree from the main application process group,
// enabling termination of mutool and all child processes via a single signal.
//
// Called by processFile() before executing mutool.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Child becomes process group leader
	}
}

// killProcessGroup kills the entire process group to ensure no orphaned children.
// Called when mutool times out or encounters an error.
//
// Uses negative PID in syscall.Kill to target the process group:
//   - cmd.Process.Pid is the process group ID (due to Setpgid)
//   - -Pid signals all processes in that group
//   - SIGKILL ensures immediate termination without possibility of being caught
//
// If the process group has already exited (indicated by ESRCH), the error is
// ignored as it is a normal condition. Other errors are returned to the caller.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process != nil && cmd.Process.Pid > 0 {
		// Signal the entire process group with SIGKILL
		// Negative PID = send to process group with PGID = |PID|
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err != syscall.ESRCH {
				return err
			}
		}
	}
	return nil
}
