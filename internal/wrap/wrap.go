// Package wrap implements terminal-multiplexer passthrough wrapping.
//
// A program running inside tmux or GNU screen does not talk to the real
// terminal: the multiplexer parses everything it writes and, by default,
// swallows escape sequences it does not understand — including OSC 52.
// Both multiplexers offer an escape hatch, a DCS envelope whose contents
// are forwarded verbatim to the outer terminal:
//
//	tmux:   ESC P tmux ; <payload with every ESC doubled> ESC \
//	screen: ESC P <payload> ESC \   (buffered, so payload must be small)
//
// screen's DCS buffer tops out around 768 bytes, so a large sequence must
// be split across many small DCS envelopes; screen strips each envelope
// and forwards the raw contents, and the outer terminal reassembles the
// original sequence from the concatenated pieces. This is the chunking
// trick every robust OSC 52 script uses, formalized here.
//
// Nesting composes: for a shell inside tmux inside screen, tmux must see
// its own envelope first, and what tmux forwards must in turn be a screen
// envelope. Chain applies the wraps in the right order; Unwrap undoes any
// combination for offline inspection of recorded streams.
package wrap

import (
	"bytes"
	"fmt"
	"strings"
)

// Mux identifies one terminal multiplexer layer.
type Mux int

const (
	// None is the absence of a multiplexer; wrapping is the identity.
	None Mux = iota
	// Tmux wraps in a "tmux;" DCS with doubled ESC bytes. Requires
	// `set -g allow-passthrough on` in tmux ≥ 3.3 (earlier versions
	// always pass DCS through).
	Tmux
	// Screen wraps in plain DCS envelopes of at most ScreenChunk bytes
	// each, because screen buffers a whole DCS before forwarding it.
	Screen
)

// String returns the lowercase mux name used by --mux and caps output.
func (m Mux) String() string {
	switch m {
	case Tmux:
		return "tmux"
	case Screen:
		return "screen"
	default:
		return "none"
	}
}

// ParseMux converts a --mux token into a Mux.
func ParseMux(s string) (Mux, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "":
		return None, nil
	case "tmux":
		return Tmux, nil
	case "screen":
		return Screen, nil
	}
	return None, fmt.Errorf("unknown multiplexer %q (want none, tmux or screen)", s)
}

// ParseChain converts a comma-separated --mux value, ordered innermost
// first (the multiplexer the process writes to directly comes first).
// "none" is only valid alone.
func ParseChain(s string) ([]Mux, error) {
	parts := strings.Split(s, ",")
	var chain []Mux
	for _, p := range parts {
		m, err := ParseMux(p)
		if err != nil {
			return nil, err
		}
		if m == None {
			if len(parts) > 1 {
				return nil, fmt.Errorf("%q: none cannot be combined with other multiplexers", s)
			}
			return nil, nil
		}
		chain = append(chain, m)
	}
	return chain, nil
}

// ChainString renders a chain for humans, innermost first.
func ChainString(chain []Mux) string {
	if len(chain) == 0 {
		return "none"
	}
	names := make([]string, len(chain))
	for i, m := range chain {
		names[i] = m.String()
	}
	return strings.Join(names, ",")
}

const (
	esc = 0x1b
	// ScreenChunk is the payload size per screen DCS envelope. screen's
	// internal DCS buffer is 768 bytes; 256 leaves generous headroom
	// for the 4-byte envelope and any future screen quirk while still
	// keeping envelope overhead under 2%.
	ScreenChunk = 256
)

var (
	tmuxOpen  = []byte("\x1bPtmux;")
	dcsOpen   = []byte("\x1bP")
	st        = []byte("\x1b\\")
	escDouble = []byte{esc, esc}
)

// TmuxWrap wraps b for tmux passthrough: a "tmux;" DCS envelope with every
// ESC in the payload doubled so tmux's parser cannot mistake payload bytes
// for the envelope's own terminator.
func TmuxWrap(b []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(b) + len(tmuxOpen) + len(st) + bytes.Count(b, []byte{esc}))
	out.Write(tmuxOpen)
	for _, c := range b {
		if c == esc {
			out.WriteByte(esc)
		}
		out.WriteByte(c)
	}
	out.Write(st)
	return out.Bytes()
}

// ScreenWrap wraps b for GNU screen: the bytes are split into envelopes of
// at most chunk payload bytes (ScreenChunk when chunk <= 0), each wrapped
// in ESC P … ESC \. screen strips the envelopes and forwards the payloads
// in order, so the outer terminal sees the original stream reassembled.
// Empty input produces empty output.
func ScreenWrap(b []byte, chunk int) []byte {
	if chunk <= 0 {
		chunk = ScreenChunk
	}
	var out bytes.Buffer
	out.Grow(len(b) + (len(b)/chunk+1)*(len(dcsOpen)+len(st)))
	for len(b) > 0 {
		n := chunk
		if n > len(b) {
			n = len(b)
		}
		out.Write(dcsOpen)
		out.Write(b[:n])
		out.Write(st)
		b = b[n:]
	}
	return out.Bytes()
}

// Wrap applies a single multiplexer layer.
func Wrap(m Mux, b []byte) []byte {
	switch m {
	case Tmux:
		return TmuxWrap(b)
	case Screen:
		return ScreenWrap(b, 0)
	default:
		return b
	}
}

// Chain wraps b for a stack of multiplexers ordered innermost first. The
// innermost multiplexer is the one that parses the process's bytes first,
// so its envelope must be the outermost layer of the final stream: for
// chain [tmux, screen] (a shell in tmux, tmux running inside screen) the
// result is TmuxWrap(ScreenWrap(b)) — tmux strips its envelope and emits a
// screen envelope, screen strips that and the terminal gets b.
func Chain(chain []Mux, b []byte) []byte {
	for i := len(chain) - 1; i >= 0; i-- {
		b = Wrap(chain[i], b)
	}
	return b
}

// ChunkCount reports how many envelopes ScreenWrap would emit for n
// payload bytes, for verbose diagnostics.
func ChunkCount(n, chunk int) int {
	if chunk <= 0 {
		chunk = ScreenChunk
	}
	if n == 0 {
		return 0
	}
	return (n + chunk - 1) / chunk
}
