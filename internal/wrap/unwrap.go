// Unwrapping multiplexer envelopes: the inverse of Chain, used by the
// decode command to inspect recorded or piped streams offline.
package wrap

import "bytes"

// unwrapTmux strips one tmux passthrough envelope from the front of b,
// un-doubling the payload's ESC bytes. ok is false when b does not start
// with a complete envelope. rest is whatever followed the envelope.
func unwrapTmux(b []byte) (inner, rest []byte, ok bool) {
	if !bytes.HasPrefix(b, tmuxOpen) {
		return nil, nil, false
	}
	body := b[len(tmuxOpen):]
	var out bytes.Buffer
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != esc {
			out.WriteByte(c)
			continue
		}
		if i+1 >= len(body) {
			return nil, nil, false // envelope cut short
		}
		switch body[i+1] {
		case esc: // doubled ESC → one payload ESC
			out.WriteByte(esc)
			i++
		case '\\': // envelope terminator
			return out.Bytes(), body[i+2:], true
		default:
			// tmux would reject this too; treat as malformed.
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// unwrapScreen strips a run of screen DCS envelopes from the front of b
// and concatenates their payloads. Payloads are raw bytes, so an ESC \
// inside a payload (e.g. the terminator of a nested tmux envelope) is
// ambiguous with the envelope's own terminator; the resolver used here —
// an ESC \ only closes the envelope when followed by another envelope
// opener or end-of-input — is exact for every stream this package itself
// produces, because chunk boundaries are always followed by ESC P or EOF.
func unwrapScreen(b []byte) (inner, rest []byte, ok bool) {
	if !bytes.HasPrefix(b, dcsOpen) || bytes.HasPrefix(b, tmuxOpen) {
		return nil, nil, false
	}
	var out bytes.Buffer
	for bytes.HasPrefix(b, dcsOpen) && !bytes.HasPrefix(b, tmuxOpen) {
		body := b[len(dcsOpen):]
		closed := false
		for i := 0; i < len(body); i++ {
			if body[i] != esc || i+1 >= len(body) || body[i+1] != '\\' {
				continue
			}
			after := body[i+2:]
			if len(after) == 0 || bytes.HasPrefix(after, dcsOpen) {
				out.Write(body[:i])
				b = after
				closed = true
				break
			}
			// ESC \ mid-payload (nested envelope terminator):
			// keep scanning for the real close.
		}
		if !closed {
			return nil, nil, false
		}
		ok = true
	}
	if !ok {
		return nil, nil, false
	}
	return out.Bytes(), b, true
}

// Unwrap removes every multiplexer envelope layer from b, in any nesting
// order, returning the innermost stream. Bytes outside envelopes are
// preserved in place, so a stream that interleaves wrapped sequences with
// plain output stays intact. Layers reports how many envelope layers were
// removed (the deepest nesting seen).
func Unwrap(b []byte) (out []byte, layers int) {
	for {
		stripped, changed := unwrapOnce(b)
		if !changed {
			return b, layers
		}
		b = stripped
		layers++
	}
}

// unwrapOnce removes one envelope layer everywhere in b.
func unwrapOnce(b []byte) ([]byte, bool) {
	var out bytes.Buffer
	changed := false
	for len(b) > 0 {
		if inner, rest, ok := unwrapTmux(b); ok {
			out.Write(inner)
			b = rest
			changed = true
			continue
		}
		if inner, rest, ok := unwrapScreen(b); ok {
			out.Write(inner)
			b = rest
			changed = true
			continue
		}
		out.WriteByte(b[0])
		b = b[1:]
	}
	return out.Bytes(), changed
}
