// Tests for environment-based capability detection. Every case builds a
// fake environment map, so nothing here depends on the machine running
// the tests.
package detect

import (
	"strings"
	"testing"

	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

func env(pairs map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := pairs[k]
		return v, ok
	}
}

func TestDetectKittyByMarkerOrTERM(t *testing.T) {
	caps := Detect(env(map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"}))
	if caps.Terminal != "kitty" || caps.Support != Yes {
		t.Fatalf("caps = %+v", caps)
	}
	if caps.MaxOSC <= DefaultMax {
		t.Fatalf("kitty budget %d should exceed the conservative default", caps.MaxOSC)
	}
	// Over SSH only TERM survives; the TERM fallback row must catch it.
	caps = Detect(env(map[string]string{"TERM": "xterm-kitty"}))
	if caps.Terminal != "kitty" || caps.Support != Yes {
		t.Fatalf("TERM fallback caps = %+v", caps)
	}
}

func TestDetectMarkerBeatsTERMMasquerade(t *testing.T) {
	// Alacritty sets TERM=xterm-256color by default; the marker
	// variable must win over the xterm row.
	caps := Detect(env(map[string]string{
		"ALACRITTY_WINDOW_ID": "7",
		"TERM":                "xterm-256color",
	}))
	if caps.Terminal != "Alacritty" || caps.Support != Yes {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestDetectAppleTerminalIsNo(t *testing.T) {
	caps := Detect(env(map[string]string{"TERM_PROGRAM": "Apple_Terminal", "TERM": "xterm-256color"}))
	if caps.Terminal != "Apple Terminal" || caps.Support != No {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestDetectITerm2IsOptIn(t *testing.T) {
	caps := Detect(env(map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm-256color"}))
	if caps.Support != OptIn {
		t.Fatalf("iTerm2 verdict = %v, want opt-in", caps.Support)
	}
	if len(caps.Notes) == 0 || !strings.Contains(caps.Notes[0], "Preferences") {
		t.Fatalf("opt-in verdict must tell the user which setting to flip: %v", caps.Notes)
	}
}

func TestDetectVTEVersionBoundary(t *testing.T) {
	old := Detect(env(map[string]string{"VTE_VERSION": "6203", "TERM": "xterm-256color"}))
	if old.Support != No {
		t.Fatalf("VTE 0.62 verdict = %v, want no", old.Support)
	}
	recent := Detect(env(map[string]string{"VTE_VERSION": "7600", "TERM": "xterm-256color"}))
	if recent.Support != Yes {
		t.Fatalf("VTE 0.76 verdict = %v, want yes", recent.Support)
	}
}

func TestDetectXtermIsOptInWithHonestNote(t *testing.T) {
	caps := Detect(env(map[string]string{"TERM": "xterm-256color"}))
	if caps.Terminal != "xterm" || caps.Support != OptIn {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestDetectLinuxConsoleAndRxvtAreNo(t *testing.T) {
	for _, term := range []string{"linux", "rxvt-unicode-256color"} {
		caps := Detect(env(map[string]string{"TERM": term}))
		if caps.Support != No {
			t.Errorf("TERM=%s verdict = %v, want no", term, caps.Support)
		}
	}
}

func TestDetectUnknownTerminalGetsConservativeDefault(t *testing.T) {
	caps := Detect(env(map[string]string{"TERM": "dumb"}))
	if caps.Terminal != "unknown" || caps.Support != Unknown || caps.MaxOSC != DefaultMax {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestMuxesSingleMultiplexerDetection(t *testing.T) {
	m := Muxes(env(map[string]string{"TMUX": "/tmp/tmux-1000/default,42,0", "TERM": "tmux-256color"}))
	if len(m) != 1 || m[0] != wrap.Tmux {
		t.Fatalf("tmux muxes = %v", m)
	}
	m = Muxes(env(map[string]string{"STY": "1234.pts-0.host", "TERM": "screen.xterm-256color"}))
	if len(m) != 1 || m[0] != wrap.Screen {
		t.Fatalf("screen muxes = %v", m)
	}
	if m := Muxes(env(map[string]string{"TERM": "xterm-256color"})); m != nil {
		t.Fatalf("plain-terminal muxes = %v, want none", m)
	}
}

func TestMuxesNestedTERMBreaksTie(t *testing.T) {
	both := map[string]string{"TMUX": "x", "STY": "y"}
	// TERM=tmux-* → tmux is innermost (it set TERM last).
	m := Muxes(env(merge(both, "TERM", "tmux-256color")))
	if wrap.ChainString(m) != "tmux,screen" {
		t.Fatalf("tmux-TERM nesting = %s", wrap.ChainString(m))
	}
	// TERM=screen* → screen is innermost.
	m = Muxes(env(merge(both, "TERM", "screen-256color")))
	if wrap.ChainString(m) != "screen,tmux" {
		t.Fatalf("screen-TERM nesting = %s", wrap.ChainString(m))
	}
}

func merge(m map[string]string, k, v string) map[string]string {
	out := map[string]string{k: v}
	for kk, vv := range m {
		out[kk] = vv
	}
	return out
}

func TestMuxesInheritedTERMOverSSHStillWraps(t *testing.T) {
	// ssh from inside tmux: TMUX does not cross, TERM does — and the
	// local tmux still parses every byte, so wrapping is required.
	m := Muxes(env(map[string]string{"TERM": "screen-256color", "SSH_TTY": "/dev/pts/3"}))
	if len(m) != 1 || m[0] != wrap.Screen {
		t.Fatalf("muxes = %v", m)
	}
}

func TestDetectSSHFlag(t *testing.T) {
	caps := Detect(env(map[string]string{"TERM": "xterm-kitty", "SSH_CONNECTION": "10.0.0.1 22 10.0.0.2 22"}))
	if !caps.SSH {
		t.Fatal("SSH_CONNECTION not detected")
	}
	caps = Detect(env(map[string]string{"TERM": "xterm-kitty"}))
	if caps.SSH {
		t.Fatal("SSH flagged without evidence")
	}
}

func TestDetectUpgradesUnknownToProbablyBehindMuxOrSSH(t *testing.T) {
	// Behind tmux (or ssh) the real terminal is invisible; "unknown"
	// would scare users away from a setup that usually works.
	caps := Detect(env(map[string]string{"TMUX": "x", "TERM": "tmux-256color"}))
	if caps.Support != Probably {
		t.Fatalf("tmux verdict = %v, want probably", caps.Support)
	}
	if !caps.WrapNeeded() {
		t.Fatal("wrap must be needed inside tmux")
	}
	caps = Detect(env(map[string]string{"TERM": "dumb", "SSH_TTY": "/dev/pts/0"}))
	if caps.Support != Probably {
		t.Fatalf("ssh verdict = %v, want probably", caps.Support)
	}
}

func TestDetectKittyInsideTmuxKeepsTerminalVerdict(t *testing.T) {
	// The marker variable survives into tmux panes; a confident verdict
	// must not be downgraded, only annotated with the tmux note.
	caps := Detect(env(map[string]string{
		"KITTY_WINDOW_ID": "1", "TMUX": "x", "TERM": "tmux-256color",
	}))
	if caps.Support != Yes || wrap.ChainString(caps.Muxes) != "tmux" {
		t.Fatalf("caps = %+v", caps)
	}
	found := false
	for _, n := range caps.Notes {
		if strings.Contains(n, "allow-passthrough") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tmux passthrough note missing: %v", caps.Notes)
	}
}

func TestSupportStringNames(t *testing.T) {
	cases := map[Support]string{
		Unknown: "unknown", No: "no", OptIn: "opt-in", Probably: "probably", Yes: "yes",
	}
	for s, want := range cases {
		if s.String() != want {
			t.Errorf("Support(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}
