// Package detect guesses terminal OSC 52 capabilities from the process
// environment, entirely offline.
//
// There is no reliable in-band way to ask "do you support OSC 52?" that
// works everywhere (the DA1/XTGETTCAP dance hangs on terminals that stay
// silent), so like every robust clipboard tool clipwarp reads the tea
// leaves the environment already provides: terminal-specific marker
// variables, TERM_PROGRAM, TERM prefixes, and the multiplexer variables
// TMUX and STY. The result is a Caps value: a support verdict, a
// conservative size limit, and the multiplexer chain that determines the
// passthrough wrapping. Everything is a pure function of a lookup
// callback, which is what makes the whole package unit-testable.
package detect

import (
	"strconv"
	"strings"

	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

// Support grades how confident the guess is.
type Support int

const (
	// Unknown means nothing in the environment identified the terminal.
	Unknown Support = iota
	// No means the identified terminal is known not to implement OSC 52.
	No
	// OptIn means the terminal implements OSC 52 but ships with it
	// disabled or restricted; the user must flip a setting.
	OptIn
	// Probably means the terminal or situation usually works but cannot
	// be confirmed from here (e.g. behind tmux the real terminal is
	// invisible).
	Probably
	// Yes means the identified terminal implements OSC 52 by default.
	Yes
)

// String returns the lowercase verdict used in caps output.
func (s Support) String() string {
	switch s {
	case No:
		return "no"
	case OptIn:
		return "opt-in"
	case Probably:
		return "probably"
	case Yes:
		return "yes"
	default:
		return "unknown"
	}
}

// DefaultMax is the conservative sequence-size limit used when the
// terminal is unknown: the classic 100 000-byte cap that hterm popularized
// and that most OSC 52 scripts inherited. Terminals in the table below
// override it when they are known to accept more.
const DefaultMax = 100000

// Caps is the full capability guess.
type Caps struct {
	Terminal string     `json:"terminal"` // human name of the identified terminal
	Via      string     `json:"via"`      // which environment evidence identified it
	Support  Support    `json:"-"`
	MaxOSC   int        `json:"max_osc_bytes"` // conservative max escape-sequence size
	Muxes    []wrap.Mux `json:"-"`             // multiplexer chain, innermost first
	SSH      bool       `json:"ssh"`
	Notes    []string   `json:"notes,omitempty"`
}

// WrapNeeded reports whether any multiplexer envelope is required.
func (c Caps) WrapNeeded() bool { return len(c.Muxes) > 0 }

type entry struct {
	name    string
	support Support
	max     int
	note    string
	// match returns the evidence string ("" = no match).
	match func(look func(string) (string, bool)) string
}

func envIs(key, want string) func(func(string) (string, bool)) string {
	return func(look func(string) (string, bool)) string {
		if v, ok := look(key); ok && v == want {
			return key + "=" + want
		}
		return ""
	}
}

func envSet(key string) func(func(string) (string, bool)) string {
	return func(look func(string) (string, bool)) string {
		if _, ok := look(key); ok {
			return key
		}
		return ""
	}
}

func termPrefix(prefix string) func(func(string) (string, bool)) string {
	return func(look func(string) (string, bool)) string {
		if v, ok := look("TERM"); ok && (v == prefix || strings.HasPrefix(v, prefix+"-")) {
			return "TERM=" + v
		}
		return ""
	}
}

// table is ordered most-specific first: marker variables beat TERM_PROGRAM
// beats TERM prefixes, because TERM=xterm-256color is what half the world
// masquerades as. Size limits are deliberately conservative write budgets,
// not exact terminal maxima.
var table = []entry{
	{name: "kitty", support: Yes, max: 8 << 20, match: envSet("KITTY_WINDOW_ID")},
	{name: "WezTerm", support: Yes, max: 1 << 20, match: envSet("WEZTERM_PANE")},
	{name: "Alacritty", support: Yes, max: 1 << 20, match: envSet("ALACRITTY_WINDOW_ID")},
	{name: "Windows Terminal", support: Yes, max: 1 << 20, match: envSet("WT_SESSION")},
	{name: "iTerm2", support: OptIn, max: 1 << 20,
		note:  "enable Preferences → General → Selection → “Applications in terminal may access clipboard”",
		match: envIs("TERM_PROGRAM", "iTerm.app")},
	{name: "Apple Terminal", support: No,
		note:  "Terminal.app has no OSC 52 support; use iTerm2 or kitty",
		match: envIs("TERM_PROGRAM", "Apple_Terminal")},
	{name: "WezTerm", support: Yes, max: 1 << 20, match: envIs("TERM_PROGRAM", "WezTerm")},
	{name: "VTE (GNOME Terminal family)", support: Yes,
		note:  "VTE supports OSC 52 copy from 0.76; paste queries are refused",
		match: vteRecent},
	{name: "VTE (GNOME Terminal family)", support: No,
		note:  "this VTE predates 0.76, which introduced OSC 52 copy",
		match: vteOld},
	{name: "Konsole", support: Yes,
		note:  "requires a Konsole release with OSC 52 enabled in the profile settings",
		match: envSet("KONSOLE_VERSION")},
	{name: "foot", support: Yes, max: 1 << 20, match: termPrefix("foot")},
	{name: "kitty", support: Yes, max: 8 << 20, match: termPrefix("xterm-kitty")},
	{name: "Alacritty", support: Yes, max: 1 << 20, match: termPrefix("alacritty")},
	{name: "WezTerm", support: Yes, max: 1 << 20, match: termPrefix("wezterm")},
	{name: "st", support: Yes, match: termPrefix("st")},
	{name: "rxvt-unicode", support: No,
		note:  "urxvt needs a perl extension (e.g. 52-osc) for OSC 52",
		match: termPrefix("rxvt")},
	{name: "Linux console", support: No,
		note:  "the kernel console has no clipboard",
		match: termPrefix("linux")},
	{name: "xterm", support: OptIn,
		note:  "xterm gates OSC 52 behind allowWindowOps/disallowedWindowOps; many distros ship it disabled — and most terminals setting TERM=xterm-* are not xterm at all",
		match: termPrefix("xterm")},
}

func vteVersion(look func(string) (string, bool)) (int, string) {
	v, ok := look("VTE_VERSION")
	if !ok {
		return 0, ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, ""
	}
	return n, "VTE_VERSION=" + v
}

func vteRecent(look func(string) (string, bool)) string {
	if n, via := vteVersion(look); n >= 7600 {
		return via
	}
	return ""
}

func vteOld(look func(string) (string, bool)) string {
	if n, via := vteVersion(look); via != "" && n < 7600 {
		return via
	}
	return ""
}

// Muxes returns the multiplexer chain implied by the environment, ordered
// innermost first. When both TMUX and STY are set the nesting order is
// ambiguous from the environment alone; TERM breaks the tie, because the
// innermost multiplexer is the one that set it (tmux defaults to
// tmux-256color since 2.1, screen always uses screen*). --mux overrides
// this heuristic when it guesses wrong.
func Muxes(look func(string) (string, bool)) []wrap.Mux {
	_, inTmux := look("TMUX")
	_, inScreen := look("STY")
	term, _ := look("TERM")
	switch {
	case inTmux && inScreen:
		if strings.HasPrefix(term, "tmux") {
			return []wrap.Mux{wrap.Tmux, wrap.Screen}
		}
		return []wrap.Mux{wrap.Screen, wrap.Tmux}
	case inTmux:
		return []wrap.Mux{wrap.Tmux}
	case inScreen:
		return []wrap.Mux{wrap.Screen}
	}
	// Without TMUX/STY, a tmux/screen TERM usually means an ssh session
	// started from inside a local multiplexer: TERM crosses ssh, TMUX
	// and STY do not. ssh is a transparent pipe, so the local
	// multiplexer still parses every byte the remote side writes —
	// wrapping is required on the remote end too.
	if strings.HasPrefix(term, "tmux") {
		return []wrap.Mux{wrap.Tmux}
	}
	if strings.HasPrefix(term, "screen") {
		return []wrap.Mux{wrap.Screen}
	}
	return nil
}

// Detect builds the full capability guess from an environment lookup
// (pass os.LookupEnv in production, a map closure in tests).
func Detect(look func(string) (string, bool)) Caps {
	caps := Caps{MaxOSC: DefaultMax}
	for _, e := range table {
		if via := e.match(look); via != "" {
			caps.Terminal = e.name
			caps.Via = via
			caps.Support = e.support
			if e.max > 0 {
				caps.MaxOSC = e.max
			}
			if e.note != "" {
				caps.Notes = append(caps.Notes, e.note)
			}
			break
		}
	}
	if caps.Terminal == "" {
		caps.Terminal = "unknown"
		if term, ok := look("TERM"); ok {
			caps.Via = "TERM=" + term
		}
	}
	caps.Muxes = Muxes(look)
	if _, ok := look("SSH_TTY"); ok {
		caps.SSH = true
	} else if _, ok := look("SSH_CONNECTION"); ok {
		caps.SSH = true
	}
	for _, m := range caps.Muxes {
		switch m {
		case wrap.Tmux:
			caps.Notes = append(caps.Notes,
				"tmux ≥ 3.3 needs `set -g allow-passthrough on`; `set -g set-clipboard on` lets tmux forward OSC 52 itself")
			if caps.Support == Unknown {
				caps.Support = Probably
				caps.Notes = append(caps.Notes,
					"the real terminal is hidden behind tmux; support depends on it")
			}
		case wrap.Screen:
			caps.Notes = append(caps.Notes,
				"screen forwards DCS envelopes of ≤ 768 bytes; clipwarp chunks automatically")
			if caps.Support == Unknown {
				caps.Support = Probably
				caps.Notes = append(caps.Notes,
					"the real terminal is hidden behind screen; support depends on it")
			}
		}
	}
	if caps.SSH && caps.Support == Unknown {
		caps.Support = Probably
		caps.Notes = append(caps.Notes,
			"over SSH the local terminal decides; OSC 52 crosses SSH unmodified")
	}
	return caps
}
