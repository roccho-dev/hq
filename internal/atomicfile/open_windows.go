//go:build windows

// Package atomicfile supplies read handles compatible with atomic file
// replacement on each supported host.
package atomicfile

import (
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	errorSharingViolation       syscall.Errno = 32
	errorLockViolation          syscall.Errno = 33
	errorUnableToRemoveReplaced syscall.Errno = 1175
	moveFileReplaceExisting                   = 0x1
)

var (
	moveFileExW  = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")
)

// OpenRead permits a writer to replace the named file while this handle is
// open. A short bounded retry covers the replacement window without turning a
// missing or inaccessible control record into an unbounded wait.
func OpenRead(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	const attempts = 25
	for attempt := 0; attempt < attempts; attempt++ {
		handle, openErr := syscall.CreateFile(
			name, syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
			nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0,
		)
		if openErr == nil {
			return os.NewFile(uintptr(handle), path), nil
		}
		if !transientOpenError(openErr) || attempt == attempts-1 {
			return nil, openErr
		}
		time.Sleep(time.Millisecond)
	}
	return nil, syscall.EINVAL
}

func transientOpenError(err error) bool {
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, errorSharingViolation) ||
		errors.Is(err, errorLockViolation) ||
		errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

// Replace atomically replaces an existing destination and also supports the
// first publication when no destination exists yet.
func Replace(temporary, destination string) error {
	from, err := syscall.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	const attempts = 25
	for attempt := 0; attempt < attempts; attempt++ {
		result, _, callErr := replaceFileW.Call(
			uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(from)), 0,
			0, 0, 0,
		)
		if result != 0 {
			return nil
		}
		if errors.Is(callErr, syscall.ERROR_FILE_NOT_FOUND) {
			return moveFirst(from, to)
		}
		if !transientReplaceError(callErr) || attempt == attempts-1 {
			if callErr != nil && callErr != syscall.Errno(0) {
				return callErr
			}
			return syscall.EINVAL
		}
		time.Sleep(time.Millisecond)
	}
	return syscall.EINVAL
}

func transientReplaceError(err error) bool {
	return errors.Is(err, errorSharingViolation) ||
		errors.Is(err, errorLockViolation) ||
		errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
		errors.Is(err, errorUnableToRemoveReplaced)
}

func moveFirst(from, to *uint16) error {
	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)),
		uintptr(moveFileReplaceExisting),
	)
	if result != 0 {
		return nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
