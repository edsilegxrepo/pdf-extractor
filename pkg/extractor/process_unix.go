//go:build !windows

package extractor

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup configures the command to run in its own process group on Unix.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup kills the entire process group to ensure no orphaned children.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process != nil && cmd.Process.Pid > 0 {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err != syscall.ESRCH {
				return err
			}
		}
	}
	return nil
}
