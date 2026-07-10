//go:build windows

package hostopen

import "os/exec"

type platformOpener struct{}

func (platformOpener) Open(path string) (Result, error) {
	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return Result{Executable: "explorer.exe", Args: []string{path}}, err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return Result{Executable: "explorer.exe", Args: []string{path}, PID: pid}, err
	}
	return Result{Executable: "explorer.exe", Args: []string{path}, PID: pid}, nil
}
