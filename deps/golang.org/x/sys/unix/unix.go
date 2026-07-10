//go:build linux

package unix

import (
	"syscall"
	"unsafe"
)

type Termios = syscall.Termios

type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

type PollFd struct {
	Fd      int32
	Events  int16
	Revents int16
}

const (
	TCGETS     = syscall.TCGETS
	TCSETS     = syscall.TCSETS
	TIOCGWINSZ = syscall.TIOCGWINSZ

	IGNBRK = syscall.IGNBRK
	BRKINT = syscall.BRKINT
	PARMRK = syscall.PARMRK
	ISTRIP = syscall.ISTRIP
	INLCR  = syscall.INLCR
	IGNCR  = syscall.IGNCR
	ICRNL  = syscall.ICRNL
	IXON   = syscall.IXON

	ECHO   = syscall.ECHO
	ECHONL = syscall.ECHONL
	ICANON = syscall.ICANON
	ISIG   = syscall.ISIG
	IEXTEN = syscall.IEXTEN

	CSIZE  = syscall.CSIZE
	PARENB = syscall.PARENB
	CS8    = syscall.CS8

	VMIN  = syscall.VMIN
	VTIME = syscall.VTIME

	POLLIN  = 0x0001
	POLLERR = 0x0008
	POLLHUP = 0x0010
)

var EINTR = syscall.EINTR

func IoctlGetTermios(fd int, req uint) (*Termios, error) {
	t := new(Termios)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return nil, errno
	}
	return t, nil
}

func IoctlSetTermios(fd int, req uint, t *Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}

func IoctlGetWinsize(fd int, req uint) (*Winsize, error) {
	ws := new(Winsize)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(ws)))
	if errno != 0 {
		return nil, errno
	}
	return ws, nil
}

func Poll(fds []PollFd, timeout int) (int, error) {
	if len(fds) == 0 {
		return 0, nil
	}
	r0, _, errno := syscall.Syscall(syscall.SYS_POLL, uintptr(unsafe.Pointer(&fds[0])), uintptr(len(fds)), uintptr(timeout))
	if errno != 0 {
		return int(r0), errno
	}
	return int(r0), nil
}

func Pipe(p []int) error                         { return syscall.Pipe(p) }
func SetNonblock(fd int, nonblocking bool) error { return syscall.SetNonblock(fd, nonblocking) }
func Close(fd int) error                         { return syscall.Close(fd) }
func Write(fd int, p []byte) (int, error)        { return syscall.Write(fd, p) }
func Read(fd int, p []byte) (int, error)         { return syscall.Read(fd, p) }
