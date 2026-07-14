//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

// Stub for platforms without termios; copy still works everywhere (it only
// writes), interactive paste politely refuses.
package tty

import "errors"

func makeRaw(_ uintptr) (func(), error) {
	return nil, errors.New("raw terminal mode is not supported on this platform; use `clipwarp paste -stdin` with a captured reply instead")
}
