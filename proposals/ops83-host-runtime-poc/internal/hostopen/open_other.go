//go:build !windows

package hostopen

import "fmt"

type platformOpener struct{}

func (platformOpener) Open(path string) (Result, error) {
	return Result{Executable: "explorer.exe", Args: []string{path}}, fmt.Errorf("host.open explorer adapter requires Windows")
}
