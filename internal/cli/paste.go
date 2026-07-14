// The paste command: send an OSC 52 query to the terminal and decode its
// reply — or, with --stdin, decode a reply captured elsewhere.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/JaydenCJ/clipwarp/internal/osc52"
	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

// pasteBufLimit bounds how much reply we will buffer before giving up:
// bigger than any sane clipboard reply, small enough that a terminal
// spewing unrelated output cannot balloon memory.
const pasteBufLimit = 32 << 20

func cmdPaste(a *App, args []string) int {
	fs := newFlagSet(a, "paste")
	target := fs.String("target", osc52.DefaultTarget, "selection target characters to query")
	primary := fs.Bool("primary", false, "shorthand for -target p (X11 primary selection)")
	mux := fs.String("mux", "auto", "multiplexer chain for wrapping the query, innermost first")
	fromStdin := fs.Bool("stdin", false, "decode a reply from stdin instead of querying the terminal")
	timeout := fs.Duration("timeout", 2*time.Second, "how long to wait for the terminal's reply")
	useST := fs.Bool("st", false, "terminate the query with ESC \\ instead of BEL")
	newline := fs.Bool("newline", false, "append a newline to the pasted bytes")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "clipwarp: paste takes no arguments")
		return ExitUsage
	}
	if *primary {
		*target = "p"
	}
	if err := osc52.ValidateTarget(*target); err != nil {
		fmt.Fprintf(a.Stderr, "clipwarp: %v\n", err)
		return ExitUsage
	}

	var stream []byte
	if *fromStdin {
		b, err := io.ReadAll(io.LimitReader(a.Stdin, pasteBufLimit))
		if err != nil {
			return fail(a, "%v", err)
		}
		stream = b
	} else {
		b, err := queryTerminal(a, *target, *mux, *useST, *timeout)
		if err != nil {
			return fail(a, "%v", err)
		}
		stream = b
	}

	// Replies captured through a multiplexer or from a trace may still
	// be wrapped; unwrap before scanning so both shapes decode.
	unwrapped, _ := wrap.Unwrap(stream)
	seq, ok := osc52.FirstWrite(unwrapped)
	if !ok {
		return fail(a, "no OSC 52 reply found (the terminal may not answer selection queries; see `clipwarp caps`)")
	}
	if _, err := a.Stdout.Write(seq.Data); err != nil {
		return fail(a, "%v", err)
	}
	if *newline {
		fmt.Fprintln(a.Stdout)
	}
	return ExitOK
}

// queryTerminal performs the interactive round trip: raw mode, wrapped
// query out, buffered reads in until a complete OSC 52 sequence (or the
// deadline) arrives.
func queryTerminal(a *App, target, mux string, useST bool, timeout time.Duration) ([]byte, error) {
	chain, err := muxChain(a, mux)
	if err != nil {
		return nil, err
	}
	term := osc52.TermBEL
	if useST {
		term = osc52.TermST
	}
	t, err := a.OpenTTY()
	if err != nil {
		return nil, fmt.Errorf("no controlling terminal (%v); run from an interactive shell or use -stdin", err)
	}
	defer t.Close()
	restore, err := t.Raw()
	if err != nil {
		return nil, err
	}
	defer restore()

	if _, err := t.Write(wrap.Chain(chain, osc52.Query(target, term))); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	_ = t.SetReadDeadline(deadline)

	var buf []byte
	chunk := make([]byte, 4096)
	for {
		n, err := t.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if osc52.Complete(buf) {
			return buf, nil
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, fmt.Errorf("no reply within %s: the terminal ignored the OSC 52 query (many terminals only implement copy)", timeout)
			}
			return nil, err
		}
		if len(buf) > pasteBufLimit {
			return nil, errors.New("reply exceeded the 32 MiB buffer limit without completing")
		}
	}
}
