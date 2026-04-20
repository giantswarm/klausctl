//go:build windows

package gatewaybridge

import (
	"os"
	"os/exec"
)

func setSysProcAttr(_ *exec.Cmd) {}

func sendTermSignal(proc *os.Process) error {
	return proc.Kill()
}

func sendKillSignal(proc *os.Process) error {
	return proc.Kill()
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(os.Signal(nil))
	return err == nil
}
