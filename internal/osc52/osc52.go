// Package osc52 builds and parses OSC 52 clipboard escape sequences.
//
// OSC 52 ("Manipulate Selection Data" in xterm's ctlseqs) is the only
// clipboard mechanism that rides the same byte stream as everything else a
// terminal prints, which is what lets it cross SSH hops for free. The wire
// format is:
//
//	ESC ] 5 2 ; <targets> ; <base64 data> <terminator>
//
// where <targets> selects one or more clipboards ("c" system clipboard,
// "p" X11 primary, "s" X11 secondary, "q" xterm's cut-buffer shorthand,
// "0".."7" the numbered cut buffers) and the terminator is either BEL
// (0x07) or the two-byte 7-bit string terminator ESC \. A data field of
// "?" queries the current selection instead of setting it, and any data
// that is not valid base64 clears the selection.
//
// Everything in this package is a pure function of its inputs; no I/O.
package osc52

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Control bytes used by the sequence grammar.
const (
	ESC = 0x1b
	BEL = 0x07
)

// Terminator selects how a built sequence is ended. BEL is the safest
// default: every OSC 52 implementation accepts it, and unlike ST it never
// contains a bare ESC that a multiplexer wrapper would have to escape.
type Terminator int

const (
	// TermBEL ends the sequence with a single 0x07 byte.
	TermBEL Terminator = iota
	// TermST ends the sequence with the 7-bit string terminator ESC \.
	TermST
)

// ValidTargets is the full set of selection characters xterm defines.
const ValidTargets = "cpqs01234567"

// DefaultTarget is the system clipboard, which is what nearly every
// terminal maps OSC 52 to regardless of the requested selection.
const DefaultTarget = "c"

// ValidateTarget reports whether target is a non-empty combination of
// characters from ValidTargets. Duplicates are allowed (terminals ignore
// them) but unknown characters are rejected early, because a bad selection
// silently no-ops on most terminals — the worst kind of failure for a
// clipboard tool.
func ValidateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("empty selection target (want characters from %q)", ValidTargets)
	}
	for _, r := range target {
		if !strings.ContainsRune(ValidTargets, r) {
			return fmt.Errorf("invalid selection character %q in target %q (want characters from %q)", r, target, ValidTargets)
		}
	}
	return nil
}

func terminator(t Terminator) string {
	if t == TermST {
		return "\x1b\\"
	}
	return "\a"
}

func build(target, payload string, t Terminator) []byte {
	var b strings.Builder
	b.Grow(len("\x1b]52;;") + len(target) + len(payload) + 2)
	b.WriteString("\x1b]52;")
	b.WriteString(target)
	b.WriteByte(';')
	b.WriteString(payload)
	b.WriteString(terminator(t))
	return []byte(b.String())
}

// Set builds a sequence that writes data to the given selection targets.
// The data is base64-encoded per the spec; empty data sets an empty
// selection (which is distinct from Clear on some terminals).
func Set(target string, data []byte, t Terminator) []byte {
	return build(target, base64.StdEncoding.EncodeToString(data), t)
}

// Clear builds a sequence that clears the given selection targets. Per
// xterm's rules any non-base64 data clears; "!" is the conventional byte.
func Clear(target string, t Terminator) []byte {
	return build(target, "!", t)
}

// Query builds a sequence that asks the terminal to report the current
// contents of the given selection targets. The terminal answers with a
// regular OSC 52 write sequence, which Scan and Complete can parse.
func Query(target string, t Terminator) []byte {
	return build(target, "?", t)
}

// EncodedLen returns the length in bytes of the full escape sequence that
// Set would produce for n bytes of data with the given target, assuming a
// BEL terminator (ST adds one byte). Used for size-limit checks before the
// payload is actually encoded.
func EncodedLen(target string, n int) int {
	return len("\x1b]52;;") + len(target) + base64.StdEncoding.EncodedLen(n) + 1
}

// MaxDataLen returns the largest number of raw payload bytes whose full
// Set sequence (BEL-terminated) still fits in limit bytes. Returns 0 when
// not even one 3-byte base64 quantum fits. Used by the truncate policy.
func MaxDataLen(target string, limit int) int {
	overhead := len("\x1b]52;;") + len(target) + 1
	room := limit - overhead
	if room < 4 {
		return 0
	}
	// base64 encodes every 3 input bytes as 4 output bytes; partial
	// quanta still occupy a full 4-byte group, so round down to whole
	// quanta for a tight bound.
	return room / 4 * 3
}
