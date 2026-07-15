//go:build !windows

package worker

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockWorkspaceApprovalFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}

func unlockWorkspaceApprovalFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
