//go:build windows

package ai

import "os/exec"

func setCmdProcessGroup(cmd *exec.Cmd) {}

func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
