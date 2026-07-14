// The caps command: print the offline capability guess, human or JSON.
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/JaydenCJ/clipwarp/internal/detect"
	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

// capsJSON is the stable machine-readable shape; scripts key off it, so
// field names are a compatibility surface.
type capsJSON struct {
	Terminal string   `json:"terminal"`
	Via      string   `json:"via,omitempty"`
	OSC52    string   `json:"osc52"`
	MaxOSC   int      `json:"max_osc_bytes"`
	Mux      string   `json:"mux"`
	WrapNeed bool     `json:"wrap_needed"`
	SSH      bool     `json:"ssh"`
	Notes    []string `json:"notes,omitempty"`
}

func cmdCaps(a *App, args []string) int {
	fs := newFlagSet(a, "caps")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	check := fs.Bool("check", false, "exit 1 when the terminal is known to lack OSC 52")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.Stderr, "clipwarp: caps takes no arguments")
		return ExitUsage
	}

	caps := detect.Detect(a.Look)
	if *asJSON {
		out := capsJSON{
			Terminal: caps.Terminal,
			Via:      caps.Via,
			OSC52:    caps.Support.String(),
			MaxOSC:   caps.MaxOSC,
			Mux:      wrap.ChainString(caps.Muxes),
			WrapNeed: caps.WrapNeeded(),
			SSH:      caps.SSH,
			Notes:    caps.Notes,
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return fail(a, "%v", err)
		}
	} else {
		via := ""
		if caps.Via != "" {
			via = " (via " + caps.Via + ")"
		}
		fmt.Fprintf(a.Stdout, "terminal       %s%s\n", caps.Terminal, via)
		fmt.Fprintf(a.Stdout, "osc52          %s\n", caps.Support)
		fmt.Fprintf(a.Stdout, "max sequence   %d bytes\n", caps.MaxOSC)
		fmt.Fprintf(a.Stdout, "multiplexer    %s\n", wrap.ChainString(caps.Muxes))
		fmt.Fprintf(a.Stdout, "wrap needed    %v\n", caps.WrapNeeded())
		fmt.Fprintf(a.Stdout, "ssh            %v\n", caps.SSH)
		for _, n := range caps.Notes {
			fmt.Fprintf(a.Stdout, "note           %s\n", n)
		}
	}
	if *check && caps.Support == detect.No {
		return ExitRuntime
	}
	return ExitOK
}
