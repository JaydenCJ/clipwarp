//go:build darwin || freebsd || netbsd || openbsd || dragonfly

// Raw mode via termios ioctls, BSD flavor (TIOCGETA/TIOCSETA).
package tty

import (
	"syscall"
	"unsafe"
)

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// makeRaw disables echo, canonical (line) mode, signal generation and CR
// translation — everything that would corrupt or delay the terminal's
// binary OSC 52 reply — while leaving output processing untouched so the
// query itself is written verbatim.
func makeRaw(fd uintptr) (func(), error) {
	var old syscall.Termios
	if err := ioctl(fd, syscall.TIOCGETA, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TIOCSETA, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(fd, syscall.TIOCSETA, unsafe.Pointer(&old)) }, nil
}
