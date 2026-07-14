// Tests for paste, caps, wrap and decode — including the interactive
// paste round trip against a scripted fake terminal.
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/clipwarp/internal/tty"
)

// fakeTerminal is a scripted controlling terminal: it records writes and
// answers reads from a canned reply, optionally split into dribbles to
// exercise the incremental read loop.
type fakeTerminal struct {
	wrote   bytes.Buffer
	replies [][]byte
	rawErr  error
	rawOn   bool
	restore bool
	closed  bool
}

func (f *fakeTerminal) Write(p []byte) (int, error) { return f.wrote.Write(p) }
func (f *fakeTerminal) Close() error                { f.closed = true; return nil }
func (f *fakeTerminal) SetReadDeadline(time.Time) error {
	return nil
}
func (f *fakeTerminal) Raw() (func(), error) {
	if f.rawErr != nil {
		return nil, f.rawErr
	}
	f.rawOn = true
	return func() { f.restore = true }, nil
}
func (f *fakeTerminal) Read(p []byte) (int, error) {
	if len(f.replies) == 0 {
		return 0, errors.New("fake terminal: no more scripted input")
	}
	chunk := f.replies[0]
	f.replies = f.replies[1:]
	n := copy(p, chunk)
	return n, nil
}

func withFake(f *fakeTerminal) func() (tty.Terminal, error) {
	return func() (tty.Terminal, error) { return f, nil }
}

func TestPasteStdinDecodesCapturedReply(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "\x1b]52;c;aGVsbG8=\a"}.run(t,
		"paste", "-stdin")
	if code != ExitOK || out != "hello" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// By default the pasted bytes are exact; -newline appends one.
	code, out, _ = harness{env: plainEnv(), stdin: "\x1b]52;c;aGk=\a"}.run(t,
		"paste", "-stdin", "-newline")
	if code != ExitOK || out != "hi\n" {
		t.Fatalf("-newline out=%q", out)
	}
}

func TestPasteStdinUnwrapsTmuxWrappedReply(t *testing.T) {
	// A reply recorded from inside tmux still carries the passthrough
	// envelope; paste must see through it.
	code, out, _ := harness{env: plainEnv(),
		stdin: "\x1bPtmux;\x1b\x1b]52;c;d3JhcHBlZA==\a\x1b\\"}.run(t, "paste", "-stdin")
	if code != ExitOK || out != "wrapped" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestPasteStdinNoSequenceExits1(t *testing.T) {
	code, _, errOut := harness{env: plainEnv(), stdin: "no escapes here"}.run(t,
		"paste", "-stdin")
	if code != ExitRuntime || !strings.Contains(errOut, "no OSC 52 reply") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestPasteInteractiveRoundTrip(t *testing.T) {
	fake := &fakeTerminal{replies: [][]byte{[]byte("\x1b]52;c;c2VjcmV0\a")}}
	code, out, errOut := harness{env: plainEnv(), openTTY: withFake(fake)}.run(t, "paste")
	if code != ExitOK || out != "secret" {
		t.Fatalf("code=%d out=%q stderr=%q", code, out, errOut)
	}
	if got := fake.wrote.String(); got != "\x1b]52;c;?\a" {
		t.Fatalf("query sent = %q", got)
	}
	if !fake.rawOn || !fake.restore || !fake.closed {
		t.Fatalf("terminal lifecycle: raw=%v restored=%v closed=%v",
			fake.rawOn, fake.restore, fake.closed)
	}
}

func TestPasteInteractiveWrapsQueryForTmux(t *testing.T) {
	fake := &fakeTerminal{replies: [][]byte{[]byte("\x1b]52;c;eA==\a")}}
	env := map[string]string{"TMUX": "sock,1,0", "TERM": "tmux-256color"}
	code, out, _ := harness{env: env, openTTY: withFake(fake)}.run(t, "paste")
	if code != ExitOK || out != "x" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if got := fake.wrote.String(); !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Fatalf("query not tmux-wrapped: %q", got)
	}
}

func TestPasteInteractiveAssemblesDribbledReply(t *testing.T) {
	// Terminals answer in arbitrary read-sized pieces; the loop must
	// keep reading until the sequence completes.
	fake := &fakeTerminal{replies: [][]byte{
		[]byte("\x1b]52;"), []byte("c;aGVs"), []byte("bG8=\a"),
	}}
	code, out, _ := harness{env: plainEnv(), openTTY: withFake(fake)}.run(t, "paste")
	if code != ExitOK || out != "hello" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestPasteInteractiveFailuresExitCleanly(t *testing.T) {
	fake := &fakeTerminal{rawErr: errors.New("termios says no")}
	code, _, errOut := harness{env: plainEnv(), openTTY: withFake(fake)}.run(t, "paste")
	if code != ExitRuntime || !strings.Contains(errOut, "termios says no") {
		t.Fatalf("raw failure: code=%d stderr=%q", code, errOut)
	}
	// Without a controlling terminal at all, the error must point at
	// the -stdin escape hatch.
	code, _, errOut = harness{env: plainEnv()}.run(t, "paste")
	if code != ExitRuntime || !strings.Contains(errOut, "-stdin") {
		t.Fatalf("no tty: code=%d stderr=%q", code, errOut)
	}
}

func TestPasteRejectsPositionalArgs(t *testing.T) {
	code, _, _ := harness{env: plainEnv()}.run(t, "paste", "extra")
	if code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestCapsHumanOutputInsideTmuxOverSSH(t *testing.T) {
	env := map[string]string{
		"TMUX": "sock,1,0", "TERM": "tmux-256color", "SSH_TTY": "/dev/pts/1",
	}
	code, out, _ := harness{env: env}.run(t, "caps")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"multiplexer    tmux", "ssh            true", "wrap needed    true", "allow-passthrough"} {
		if !strings.Contains(out, want) {
			t.Fatalf("caps output missing %q:\n%s", want, out)
		}
	}
}

func TestCapsJSONShape(t *testing.T) {
	env := map[string]string{"KITTY_WINDOW_ID": "3", "TERM": "xterm-kitty"}
	code, out, _ := harness{env: env}.run(t, "caps", "-json")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var got struct {
		Terminal string `json:"terminal"`
		OSC52    string `json:"osc52"`
		MaxOSC   int    `json:"max_osc_bytes"`
		Mux      string `json:"mux"`
		WrapNeed bool   `json:"wrap_needed"`
		SSH      bool   `json:"ssh"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Terminal != "kitty" || got.OSC52 != "yes" || got.Mux != "none" || got.WrapNeed || got.SSH {
		t.Fatalf("json = %+v", got)
	}
	if got.MaxOSC != 8<<20 {
		t.Fatalf("max_osc_bytes = %d", got.MaxOSC)
	}
}

func TestCapsCheckExitCodes(t *testing.T) {
	code, _, _ := harness{env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}}.run(t,
		"caps", "-check")
	if code != ExitRuntime {
		t.Fatalf("Apple Terminal -check code=%d, want 1", code)
	}
	code, _, _ = harness{env: plainEnv()}.run(t, "caps", "-check")
	if code != ExitOK {
		t.Fatalf("kitty -check code=%d, want 0", code)
	}
}

func TestWrapFilterAppliesEnvelope(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "raw-bytes"}.run(t,
		"wrap", "-mux", "tmux")
	if code != ExitOK || out != "\x1bPtmux;raw-bytes\x1b\\" {
		t.Fatalf("explicit mux: code=%d out=%q", code, out)
	}
	// Without -mux the environment decides, exactly like copy.
	env := map[string]string{"STY": "1.pts-0.h", "TERM": "screen"}
	code, out, _ = harness{env: env, stdin: "abc"}.run(t, "wrap")
	if code != ExitOK || out != "\x1bPabc\x1b\\" {
		t.Fatalf("auto mux: code=%d out=%q", code, out)
	}
}

func TestWrapUndoRemovesEnvelopes(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "\x1bPtmux;inner\x1b\\"}.run(t,
		"wrap", "-undo")
	if code != ExitOK || out != "inner" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestDecodeFirstByDefaultAllOnRequest(t *testing.T) {
	stdin := "\x1b]52;c;Zmlyc3Q=\a middle \x1b]52;c;c2Vjb25k\a"
	code, out, _ := harness{env: plainEnv(), stdin: stdin}.run(t, "decode")
	if code != ExitOK || out != "first" {
		t.Fatalf("default: code=%d out=%q", code, out)
	}
	code, out, _ = harness{env: plainEnv(), stdin: stdin}.run(t, "decode", "-all")
	if code != ExitOK || out != "firstsecond" {
		t.Fatalf("-all: code=%d out=%q", code, out)
	}
}

func TestDecodeJSONReportsKindAndTarget(t *testing.T) {
	stdin := "\x1b]52;p;?\a\x1b]52;c;ZGF0YQ==\a"
	code, out, _ := harness{env: plainEnv(), stdin: stdin}.run(t, "decode", "-all", "-json")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSON lines, got %d: %q", len(lines), out)
	}
	var first, second struct {
		Target string `json:"target"`
		Kind   string `json:"kind"`
		Data   string `json:"data_base64"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first.Kind != "query" || first.Target != "p" || second.Kind != "write" || second.Data != "ZGF0YQ==" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestDecodeFailureDiagnostics(t *testing.T) {
	code, _, errOut := harness{env: plainEnv(), stdin: "nothing to see"}.run(t, "decode")
	if code != ExitRuntime || !strings.Contains(errOut, "no OSC 52 sequences") {
		t.Fatalf("empty: code=%d stderr=%q", code, errOut)
	}
	// A never-terminated sequence must be called out, not dropped.
	code, _, errOut = harness{env: plainEnv(), stdin: "\x1b]52;c;dHJ1bmNhdGVk"}.run(t, "decode")
	if code != ExitRuntime || !strings.Contains(errOut, "malformed") {
		t.Fatalf("malformed: code=%d stderr=%q", code, errOut)
	}
}

func TestCopyPasteRoundTripThroughNestedMuxes(t *testing.T) {
	// The whole pipeline: copy inside screen-inside-tmux, then decode
	// the doubly-wrapped stream back to the original bytes.
	payload := "round trip 🚀 with unicode\n\tand tabs"
	env := map[string]string{"TMUX": "s,1,0", "STY": "9.pts-1.h", "TERM": "screen-256color"}
	code, wire, _ := harness{env: env, stdin: payload}.run(t, "copy", "-out", "-")
	if code != ExitOK {
		t.Fatalf("copy code=%d", code)
	}
	code, out, _ := harness{env: plainEnv(), stdin: wire}.run(t, "paste", "-stdin")
	if code != ExitOK || out != payload {
		t.Fatalf("round trip: code=%d out=%q", code, out)
	}
}
