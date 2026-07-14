// The wrap command: a byte filter that applies (or removes) multiplexer
// envelopes, for people gluing OSC 52 into their own scripts.
package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

func cmdWrap(a *App, args []string) int {
	fs := newFlagSet(a, "wrap")
	mux := fs.String("mux", "auto", "multiplexer chain to wrap for, innermost first")
	undo := fs.Bool("undo", false, "remove envelopes instead of adding them")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "clipwarp: wrap reads stdin and takes no arguments")
		return ExitUsage
	}

	b, err := io.ReadAll(a.Stdin)
	if err != nil {
		return fail(a, "%v", err)
	}
	if *undo {
		out, _ := wrap.Unwrap(b)
		if _, err := a.Stdout.Write(out); err != nil {
			return fail(a, "%v", err)
		}
		return ExitOK
	}
	chain, err := muxChain(a, *mux)
	if err != nil {
		fmt.Fprintf(a.Stderr, "clipwarp: %v\n", err)
		return ExitUsage
	}
	if _, err := a.Stdout.Write(wrap.Chain(chain, b)); err != nil {
		return fail(a, "%v", err)
	}
	return ExitOK
}
