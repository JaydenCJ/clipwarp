// The copy command: read bytes, build an OSC 52 set sequence, enforce the
// size budget, wrap for any multiplexers, and deliver it to the terminal.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/JaydenCJ/clipwarp/internal/detect"
	"github.com/JaydenCJ/clipwarp/internal/osc52"
	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

func cmdCopy(a *App, args []string) int {
	fs := newFlagSet(a, "copy")
	target := fs.String("target", osc52.DefaultTarget, "selection target characters (c, p, s, q, 0-7; combinable)")
	primary := fs.Bool("primary", false, "shorthand for -target p (X11 primary selection)")
	mux := fs.String("mux", "auto", "multiplexer chain, innermost first: auto, none, tmux, screen, or e.g. tmux,screen")
	outPath := fs.String("out", "auto", "where to write the sequence: auto (controlling terminal, else stdout), - (stdout), or a path")
	maxBytes := fs.Int("max-bytes", 0, "sequence size budget in bytes (0 = detected terminal limit)")
	onOversize := fs.String("on-oversize", "error", "what to do when the sequence exceeds the budget: error, truncate or force")
	clear := fs.Bool("clear", false, "clear the selection instead of setting it")
	useST := fs.Bool("st", false, "terminate with ESC \\ instead of BEL")
	trim := fs.Bool("trim", false, "drop one trailing newline from the input (for echo/heredoc pipelines)")
	dryRun := fs.Bool("dry-run", false, "print the sequence with visible escapes instead of emitting it")
	verbose := fs.Bool("verbose", false, "report sizes, chunking and wrapping on stderr")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}

	if *primary {
		*target = "p"
	}
	if err := osc52.ValidateTarget(*target); err != nil {
		fmt.Fprintf(a.Stderr, "clipwarp: %v\n", err)
		return ExitUsage
	}
	if *onOversize != "error" && *onOversize != "truncate" && *onOversize != "force" {
		fmt.Fprintf(a.Stderr, "clipwarp: -on-oversize must be error, truncate or force, not %q\n", *onOversize)
		return ExitUsage
	}
	chain, err := muxChain(a, *mux)
	if err != nil {
		fmt.Fprintf(a.Stderr, "clipwarp: %v\n", err)
		return ExitUsage
	}

	term := osc52.TermBEL
	if *useST {
		term = osc52.TermST
	}

	var data []byte
	if !*clear {
		data, err = readInput(a, fs.Args())
		if err != nil {
			return fail(a, "%v", err)
		}
		if *trim && len(data) > 0 && data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
		}
	} else if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "clipwarp: -clear takes no file arguments")
		return ExitUsage
	}

	limit := *maxBytes
	if limit <= 0 {
		limit = detect.Detect(a.Look).MaxOSC
	}

	truncated := false
	if !*clear && osc52.EncodedLen(*target, len(data)) > limit {
		switch *onOversize {
		case "error":
			return fail(a, "payload of %d bytes makes a %d-byte sequence, over the %d-byte budget (see `clipwarp caps`; -max-bytes overrides, -on-oversize truncate|force proceeds)",
				len(data), osc52.EncodedLen(*target, len(data)), limit)
		case "truncate":
			keep := osc52.MaxDataLen(*target, limit)
			if keep <= 0 {
				return fail(a, "budget of %d bytes cannot fit any payload", limit)
			}
			data = data[:keep]
			truncated = true
		case "force":
			// The user knows their terminal better than the table.
		}
	}

	var seq []byte
	if *clear {
		seq = osc52.Clear(*target, term)
	} else {
		seq = osc52.Set(*target, data, term)
	}
	out := wrap.Chain(chain, seq)

	if *verbose {
		// The chunk count depends on what screen sees at its layer, so
		// mirror Chain's outermost-envelope-first application order.
		chunks := 0
		layer := seq
		for i := len(chain) - 1; i >= 0; i-- {
			if chain[i] == wrap.Screen {
				chunks = wrap.ChunkCount(len(layer), 0)
			}
			layer = wrap.Wrap(chain[i], layer)
		}
		fmt.Fprintf(a.Stderr, "clipwarp: target=%s payload=%d sequence=%d budget=%d mux=%s wrapped=%d chunks=%d truncated=%v\n",
			*target, len(data), len(seq), limit, wrap.ChainString(chain), len(out), chunks, truncated)
	}
	if truncated {
		fmt.Fprintf(a.Stderr, "clipwarp: warning: payload truncated to %d bytes to fit the %d-byte budget\n", len(data), limit)
	}

	if *dryRun {
		fmt.Fprintln(a.Stdout, escapeVisible(out))
		return ExitOK
	}
	if err := emit(a, *outPath, out); err != nil {
		return fail(a, "%v", err)
	}
	return ExitOK
}

// readInput concatenates the named files, or reads stdin when none are
// given — mirroring cat, so `clipwarp copy notes.txt` and
// `git diff | clipwarp copy` both do the obvious thing.
func readInput(a *App, files []string) ([]byte, error) {
	if len(files) == 0 {
		return io.ReadAll(a.Stdin)
	}
	var data []byte
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		data = append(data, b...)
	}
	return data, nil
}

// emit delivers the final byte stream. "auto" prefers the controlling
// terminal so `clipwarp copy` works mid-pipeline without corrupting the
// pipe, falling back to stdout when there is no terminal (tests, CI).
func emit(a *App, outPath string, b []byte) error {
	switch outPath {
	case "-":
		_, err := a.Stdout.Write(b)
		return err
	case "auto":
		if t, err := a.OpenTTY(); err == nil {
			defer t.Close()
			_, werr := t.Write(b)
			return werr
		}
		_, err := a.Stdout.Write(b)
		return err
	default:
		f, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(b)
		return err
	}
}
