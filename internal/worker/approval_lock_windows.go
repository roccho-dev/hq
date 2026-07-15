//go:build windows

package worker

import (
	"os"
	"syscall"
	"unsafe"
)

const lockFileExclusiveLock = 0x2

var (
	lockFileExW   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlockFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func lockWorkspaceApprovalFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileExW.Call(
		file.Fd(), uintptr(lockFileExclusiveLock), 0, 1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result != 0 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}

func unlockWorkspaceApprovalFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := unlockFileExW.Call(
		file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)),
	)
	if result != 0 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
