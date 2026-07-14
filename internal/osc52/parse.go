// OSC 52 stream parsing: locating, classifying and decoding sequences in
// arbitrary byte streams (terminal replies, recorded traces, piped input).
package osc52

import (
	"bytes"
	"encoding/base64"
)

// Kind classifies a parsed OSC 52 sequence.
type Kind int

const (
	// KindWrite carries base64 selection data (a set, or a terminal's
	// reply to a query).
	KindWrite Kind = iota
	// KindQuery asks the terminal to report the selection ("?").
	KindQuery
	// KindClear carries non-base64 data, which clears the selection.
	KindClear
)

// String returns the lowercase name of the kind, used in --json output.
func (k Kind) String() string {
	switch k {
	case KindQuery:
		return "query"
	case KindClear:
		return "clear"
	default:
		return "write"
	}
}

// Sequence is one parsed OSC 52 sequence.
type Sequence struct {
	Target string // selection characters, e.g. "c" or "pc"
	Kind   Kind
	Data   []byte // decoded payload; nil unless Kind == KindWrite
	Raw    string // payload field exactly as it appeared on the wire
}

var seqPrefix = []byte("\x1b]52;")

// index locates the first complete OSC 52 sequence in b. end is exclusive
// and includes the terminator. found reports whether a prefix was seen at
// all; complete reports whether its terminator arrived — the distinction
// lets an interactive reader keep waiting on a partial reply.
func index(b []byte) (start, end int, found, complete bool) {
	start = bytes.Index(b, seqPrefix)
	if start < 0 {
		return 0, 0, false, false
	}
	body := b[start+len(seqPrefix):]
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case BEL:
			return start, start + len(seqPrefix) + i + 1, true, true
		case 0x9c: // 8-bit ST, emitted by a few terminals in replies
			return start, start + len(seqPrefix) + i + 1, true, true
		case ESC:
			if i+1 < len(body) && body[i+1] == '\\' {
				return start, start + len(seqPrefix) + i + 2, true, true
			}
			// A bare ESC that does not open ST aborts this
			// sequence (the terminal would too); the caller
			// resumes scanning after the prefix.
			return start, 0, true, false
		}
	}
	return start, 0, true, false
}

// Complete reports whether b contains at least one complete OSC 52
// sequence. An interactive paste loop calls this after every read to know
// when the terminal's reply has fully arrived.
func Complete(b []byte) bool {
	for {
		start, _, found, complete := index(b)
		if !found {
			return false
		}
		if complete {
			return true
		}
		// Incomplete: skip past this prefix and look for another.
		b = b[start+len(seqPrefix):]
	}
}

func parseBody(body []byte) (Sequence, bool) {
	sep := bytes.IndexByte(body, ';')
	if sep < 0 {
		return Sequence{}, false
	}
	target := string(body[:sep])
	if ValidateTarget(target) != nil {
		return Sequence{}, false
	}
	raw := string(body[sep+1:])
	seq := Sequence{Target: target, Raw: raw}
	if raw == "?" {
		seq.Kind = KindQuery
		return seq, true
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Per xterm semantics, non-base64 data clears the selection
		// rather than erroring — mirror that instead of rejecting.
		seq.Kind = KindClear
		return seq, true
	}
	seq.Kind = KindWrite
	seq.Data = data
	return seq, true
}

// stripTerminator removes the trailing BEL, 8-bit ST or ESC \ from a
// complete sequence body slice.
func stripTerminator(b []byte) []byte {
	if n := len(b); n > 0 {
		switch b[n-1] {
		case BEL, 0x9c:
			return b[:n-1]
		case '\\':
			if n > 1 && b[n-2] == ESC {
				return b[:n-2]
			}
		}
	}
	return b
}

// Scan finds every complete OSC 52 sequence in b, in order. Bytes between
// sequences are ignored (a real terminal stream interleaves prompts,
// colors and application output around the sequence). malformed counts
// prefixes that never completed or had an unparseable body — surfaced so
// callers can warn instead of silently dropping data.
func Scan(b []byte) (seqs []Sequence, malformed int) {
	for len(b) > 0 {
		start, end, found, complete := index(b)
		if !found {
			break
		}
		if !complete {
			malformed++
			b = b[start+len(seqPrefix):]
			continue
		}
		body := stripTerminator(b[start+len(seqPrefix) : end])
		if seq, ok := parseBody(body); ok {
			seqs = append(seqs, seq)
		} else {
			malformed++
		}
		b = b[end:]
	}
	return seqs, malformed
}

// FirstWrite returns the first KindWrite sequence in b, which is how a
// paste reply is extracted from whatever else the terminal echoed around
// it. ok is false when the stream contains no write sequence.
func FirstWrite(b []byte) (Sequence, bool) {
	seqs, _ := Scan(b)
	for _, s := range seqs {
		if s.Kind == KindWrite {
			return s, true
		}
	}
	return Sequence{}, false
}
