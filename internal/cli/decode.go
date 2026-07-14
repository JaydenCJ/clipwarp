// The decode command: pull OSC 52 sequences out of an arbitrary byte
// stream (a recorded trace, `tmux capture-pane -e`, a debug pipe), unwrap
// any multiplexer envelopes, and print what they carry.
package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/JaydenCJ/clipwarp/internal/osc52"
	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

type decodedJSON struct {
	Target string `json:"target"`
	Kind   string `json:"kind"`
	Bytes  int    `json:"bytes"`
	Data   string `json:"data_base64,omitempty"`
}

func cmdDecode(a *App, args []string) int {
	fs := newFlagSet(a, "decode")
	all := fs.Bool("all", false, "print every sequence's payload, not just the first")
	asJSON := fs.Bool("json", false, "emit one JSON object per sequence")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "clipwarp: decode reads stdin and takes no arguments")
		return ExitUsage
	}

	b, err := io.ReadAll(a.Stdin)
	if err != nil {
		return fail(a, "%v", err)
	}
	unwrapped, layers := wrap.Unwrap(b)
	seqs, malformed := osc52.Scan(unwrapped)
	if malformed > 0 {
		fmt.Fprintf(a.Stderr, "clipwarp: warning: skipped %s\n", plural(malformed, "malformed OSC 52 sequence"))
	}
	if len(seqs) == 0 {
		return fail(a, "no OSC 52 sequences in input (unwrapped %s)", plural(layers, "envelope layer"))
	}

	if *asJSON {
		enc := json.NewEncoder(a.Stdout)
		for i, s := range seqs {
			if !*all && i > 0 {
				break
			}
			out := decodedJSON{Target: s.Target, Kind: s.Kind.String(), Bytes: len(s.Data)}
			if s.Kind == osc52.KindWrite {
				out.Data = base64.StdEncoding.EncodeToString(s.Data)
			}
			if err := enc.Encode(out); err != nil {
				return fail(a, "%v", err)
			}
		}
		return ExitOK
	}
	for i, s := range seqs {
		if !*all && i > 0 {
			break
		}
		switch s.Kind {
		case osc52.KindWrite:
			if _, err := a.Stdout.Write(s.Data); err != nil {
				return fail(a, "%v", err)
			}
		case osc52.KindQuery:
			fmt.Fprintf(a.Stderr, "clipwarp: sequence %d is a query for target %q (no payload)\n", i+1, s.Target)
		case osc52.KindClear:
			fmt.Fprintf(a.Stderr, "clipwarp: sequence %d clears target %q (no payload)\n", i+1, s.Target)
		}
	}
	return ExitOK
}
