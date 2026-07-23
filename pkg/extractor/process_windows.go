//go:build windows

package extractor

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to create a new process group on Windows.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessGroup terminates the mutool process on Windows.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return nil
}
