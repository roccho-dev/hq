//go:build !windows

package worker

import (
	"os"
	"syscall"
)

func lockWorkspaceApprovalFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockWorkspaceApprovalFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
