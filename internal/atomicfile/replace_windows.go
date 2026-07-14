//go:build windows

package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const errorSharingViolation = syscall.Errno(32)

var replaceFileW = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")

func replace(oldPath, newPath string) error {
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		err := os.Rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if !isSharingViolation(err) {
			return err
		}
		if replaceErr := replaceExisting(newPath, oldPath); replaceErr == nil {
			return nil
		} else if !isSharingViolation(replaceErr) {
			return replaceErr
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

func replaceExisting(existingPath, replacementPath string) error {
	existing, err := syscall.UTF16PtrFromString(extendedPath(existingPath))
	if err != nil {
		return err
	}
	replacement, err := syscall.UTF16PtrFromString(extendedPath(replacementPath))
	if err != nil {
		return err
	}
	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(existing)),
		uintptr(unsafe.Pointer(replacement)),
		0,
		0,
		0,
		0,
	)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}

func read(path string) ([]byte, error) {
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		data, err := readShared(path)
		if err == nil {
			return data, nil
		}
		if !isSharingViolation(err) {
			return nil, err
		}
		if !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(time.Millisecond)
	}
}

func readShared(path string) ([]byte, error) {
	file, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func openShared(path string) (*os.File, error) {
	pointer, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		syscall.CloseHandle(handle)
		return nil, errors.New("open shared file")
	}
	return file, nil
}

func extendedPath(path string) string {
	if len(path) < 248 || strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\??\`) || strings.HasPrefix(path, `\\.\`) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`)
	}
	return `\\?\` + absolute
}

func isSharingViolation(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errorSharingViolation)
}

func remove(path string) error {
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		err := os.Remove(path)
		if err == nil || !isSharingViolation(err) {
			return err
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}
