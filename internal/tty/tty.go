// Package tty gives interactive commands a direct line to the controlling
// terminal, bypassing whatever stdin/stdout are redirected to. The paste
// command needs it twice over: the OSC 52 query must reach the terminal
// even when stdout is a pipe, and the terminal's reply arrives as input
// bytes that must be read without echo or line buffering.
package tty

import (
	"os"
	"time"
)

// Terminal is the minimal surface the paste command needs; the CLI accepts
// any implementation so tests can substitute a scripted fake.
type Terminal interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	// Raw switches the terminal to raw-enough mode (no echo, no line
	// buffering) and returns a restore function. Restore must run even
	// on error paths — a terminal left raw eats the user's shell.
	Raw() (restore func(), err error)
	// SetReadDeadline bounds a Read, so a terminal that never answers
	// the query cannot hang paste forever.
	SetReadDeadline(t time.Time) error
}

// device adapts an os.File for the controlling terminal.
type device struct{ f *os.File }

// Open opens the controlling terminal read-write. It fails when the
// process has no controlling terminal (cron, CI, a detached daemon), which
// callers surface as "run this from an interactive terminal".
func Open() (Terminal, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &device{f: f}, nil
}

func (d *device) Read(p []byte) (int, error)        { return d.f.Read(p) }
func (d *device) Write(p []byte) (int, error)       { return d.f.Write(p) }
func (d *device) Close() error                      { return d.f.Close() }
func (d *device) SetReadDeadline(t time.Time) error { return d.f.SetReadDeadline(t) }
func (d *device) Raw() (func(), error)              { return makeRaw(d.f.Fd()) }
