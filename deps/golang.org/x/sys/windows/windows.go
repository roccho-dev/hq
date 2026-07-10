//go:build windows

package windows

import (
	"syscall"
	"unsafe"
)

type Handle = syscall.Handle

type Coord struct {
	X int16
	Y int16
}

type SmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type ConsoleScreenBufferInfo struct {
	Size              Coord
	CursorPosition    Coord
	Attributes        uint16
	Window            SmallRect
	MaximumWindowSize Coord
}

const (
	ENABLE_ECHO_INPUT       = 0x0004
	ENABLE_LINE_INPUT       = 0x0002
	ENABLE_PROCESSED_INPUT  = 0x0001
	ENABLE_PROCESSED_OUTPUT = 0x0001
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

func GetConsoleMode(h Handle, mode *uint32) error {
	r1, _, e1 := syscall.Syscall(procGetConsoleMode.Addr(), 2, uintptr(h), uintptr(unsafe.Pointer(mode)), 0)
	if r1 == 0 {
		if e1 != 0 {
			return e1
		}
		return syscall.EINVAL
	}
	return nil
}

func SetConsoleMode(h Handle, mode uint32) error {
	r1, _, e1 := syscall.Syscall(procSetConsoleMode.Addr(), 2, uintptr(h), uintptr(mode), 0)
	if r1 == 0 {
		if e1 != 0 {
			return e1
		}
		return syscall.EINVAL
	}
	return nil
}

func GetConsoleScreenBufferInfo(h Handle, info *ConsoleScreenBufferInfo) error {
	r1, _, e1 := syscall.Syscall(procGetConsoleScreenBufferInfo.Addr(), 2, uintptr(h), uintptr(unsafe.Pointer(info)), 0)
	if r1 == 0 {
		if e1 != 0 {
			return e1
		}
		return syscall.EINVAL
	}
	return nil
}
