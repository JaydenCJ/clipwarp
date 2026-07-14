// End-to-end tests for the CLI, run in-process: real flag parsing, real
// stdin/stdout, a fake environment, and a scripted fake terminal for the
// interactive paste path.
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/clipwarp/internal/tty"
	"github.com/JaydenCJ/clipwarp/internal/version"
)

// harness runs the CLI once and captures everything.
type harness struct {
	env     map[string]string
	stdin   string
	openTTY func() (tty.Terminal, error)
}

func (h harness) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	open := h.openTTY
	if open == nil {
		open = func() (tty.Terminal, error) { return nil, errors.New("no tty in tests") }
	}
	a := &App{
		Stdin:  strings.NewReader(h.stdin),
		Stdout: &out,
		Stderr: &errBuf,
		Look: func(k string) (string, bool) {
			v, ok := h.env[k]
			return v, ok
		},
		OpenTTY: open,
	}
	return Run(a, args), out.String(), errBuf.String()
}

func plainEnv() map[string]string {
	return map[string]string{"TERM": "xterm-kitty"}
}

func TestVersionCommand(t *testing.T) {
	code, out, _ := harness{env: plainEnv()}.run(t, "version")
	if code != ExitOK || out != "clipwarp "+version.Version+"\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestUsageErrorsAllExit2(t *testing.T) {
	code, _, errOut := harness{env: plainEnv()}.run(t)
	if code != ExitUsage || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("no args: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = harness{env: plainEnv()}.run(t, "frobnicate")
	if code != ExitUsage || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown command: code=%d stderr=%q", code, errOut)
	}
	code, _, _ = harness{env: plainEnv(), stdin: "x"}.run(t, "copy", "-bogus")
	if code != ExitUsage {
		t.Fatalf("unknown flag: code=%d", code)
	}
}

func TestCopyStdinToStdoutExactBytes(t *testing.T) {
	code, out, errOut := harness{env: plainEnv(), stdin: "hello"}.run(t,
		"copy", "-mux", "none", "-out", "-")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if out != "\x1b]52;c;aGVsbG8=\a" {
		t.Fatalf("out=%q", out)
	}
	code, out, _ = harness{env: plainEnv(), stdin: "hi"}.run(t,
		"copy", "-st", "-mux", "none", "-out", "-")
	if code != ExitOK || !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("ST terminator out=%q", out)
	}
}

func TestCopyAutoWrapsInsideTmux(t *testing.T) {
	env := map[string]string{"TMUX": "/tmp/sock,1,0", "TERM": "tmux-256color"}
	code, out, _ := harness{env: env, stdin: "hi"}.run(t, "copy", "-out", "-")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	want := "\x1bPtmux;\x1b\x1b]52;c;aGk=\a\x1b\\"
	if out != want {
		t.Fatalf("out=%q want %q", out, want)
	}
}

func TestCopyAutoChunksInsideScreen(t *testing.T) {
	env := map[string]string{"STY": "99.pts-0.box", "TERM": "screen-256color"}
	big := strings.Repeat("q", 1200) // encodes to 1600 b64 chars → several chunks
	code, out, _ := harness{env: env, stdin: big}.run(t, "copy", "-out", "-")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if n := strings.Count(out, "\x1bP"); n < 4 {
		t.Fatalf("expected several screen envelopes, got %d", n)
	}
	// The chunked stream must decode back to the original payload.
	code, decoded, _ := harness{env: plainEnv(), stdin: out}.run(t, "decode")
	if code != ExitOK || decoded != big {
		t.Fatalf("decode of chunked stream failed: code=%d len=%d", code, len(decoded))
	}
}

func TestCopyExplicitNestedChain(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "x"}.run(t,
		"copy", "-mux", "tmux,screen", "-out", "-")
	if code != ExitOK || !strings.HasPrefix(out, "\x1bPtmux;") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, decoded, _ := harness{env: plainEnv(), stdin: out}.run(t, "decode")
	if code != ExitOK || decoded != "x" {
		t.Fatalf("nested chain did not survive decode: %q", decoded)
	}
}

func TestCopyReadsFilesInOrder(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("one"), 0o644)
	os.WriteFile(f2, []byte("two"), 0o644)
	code, out, _ := harness{env: plainEnv()}.run(t,
		"copy", "-mux", "none", "-out", "-", f1, f2)
	if code != ExitOK || out != "\x1b]52;c;b25ldHdv\a" { // base64("onetwo")
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCopyMissingFileExits1(t *testing.T) {
	code, _, errOut := harness{env: plainEnv()}.run(t,
		"copy", "-out", "-", filepath.Join(t.TempDir(), "absent.txt"))
	if code != ExitRuntime || !strings.Contains(errOut, "absent.txt") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestCopyTargetSelection(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "x"}.run(t,
		"copy", "-primary", "-mux", "none", "-out", "-")
	if code != ExitOK || !strings.HasPrefix(out, "\x1b]52;p;") {
		t.Fatalf("-primary out=%q", out)
	}
	code, _, errOut := harness{env: plainEnv(), stdin: "x"}.run(t,
		"copy", "-target", "z", "-out", "-")
	if code != ExitUsage || !strings.Contains(errOut, "invalid selection") {
		t.Fatalf("bad target: code=%d stderr=%q", code, errOut)
	}
}

func TestCopyClearEmitsClearSequence(t *testing.T) {
	code, out, _ := harness{env: plainEnv()}.run(t,
		"copy", "-clear", "-mux", "none", "-out", "-")
	if code != ExitOK || out != "\x1b]52;c;!\a" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestCopyTrimDropsOneTrailingNewline(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "hi\n"}.run(t,
		"copy", "-trim", "-mux", "none", "-out", "-")
	if code != ExitOK || out != "\x1b]52;c;aGk=\a" {
		t.Fatalf("out=%q", out)
	}
}

func TestCopyOversizeErrorsByDefault(t *testing.T) {
	code, out, errOut := harness{env: plainEnv(), stdin: strings.Repeat("a", 100)}.run(t,
		"copy", "-max-bytes", "50", "-out", "-")
	if code != ExitRuntime || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(errOut, "budget") {
		t.Fatalf("stderr should explain the budget: %q", errOut)
	}
}

func TestCopyOversizeTruncateFitsBudgetAndWarns(t *testing.T) {
	code, out, errOut := harness{env: plainEnv(), stdin: strings.Repeat("a", 100)}.run(t,
		"copy", "-max-bytes", "50", "-on-oversize", "truncate", "-mux", "none", "-out", "-")
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if len(out) > 50 {
		t.Fatalf("truncated sequence is %d bytes, over the 50-byte budget", len(out))
	}
	if !strings.Contains(errOut, "truncated") {
		t.Fatalf("missing truncation warning: %q", errOut)
	}
	code, decoded, _ := harness{env: plainEnv(), stdin: out}.run(t, "decode")
	if code != ExitOK || !strings.HasPrefix(decoded, "aaa") {
		t.Fatalf("truncated payload not decodable: %q", decoded)
	}
}

func TestCopyOversizeForceEmitsAnyway(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: strings.Repeat("a", 100)}.run(t,
		"copy", "-max-bytes", "50", "-on-oversize", "force", "-mux", "none", "-out", "-")
	if code != ExitOK || len(out) <= 50 {
		t.Fatalf("force did not emit the full sequence: code=%d len=%d", code, len(out))
	}
}

func TestCopyOversizeUsesDetectedTerminalBudget(t *testing.T) {
	// kitty's 8 MiB budget must let through what the 100 KB default
	// would reject — the whole point of per-terminal limits.
	big := strings.Repeat("z", 200_000)
	code, _, _ := harness{env: plainEnv(), stdin: big}.run(t, "copy", "-mux", "none", "-out", "-")
	if code != ExitOK {
		t.Fatalf("kitty budget rejected 200 KB: code=%d", code)
	}
	code, _, errOut := harness{env: map[string]string{"TERM": "dumb"}, stdin: big}.run(t,
		"copy", "-mux", "none", "-out", "-")
	if code != ExitRuntime || !strings.Contains(errOut, "100000") {
		t.Fatalf("default budget not enforced: code=%d stderr=%q", code, errOut)
	}
}

func TestCopyInvalidOnOversizeValueExits2(t *testing.T) {
	code, _, _ := harness{env: plainEnv(), stdin: "x"}.run(t,
		"copy", "-on-oversize", "maybe", "-out", "-")
	if code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestCopyDryRunPrintsVisibleEscapes(t *testing.T) {
	code, out, _ := harness{env: plainEnv(), stdin: "hi"}.run(t,
		"copy", "-dry-run", "-mux", "tmux")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	want := `\ePtmux;\e\e]52;c;aGk=\a\e\` + "\n"
	if out != want {
		t.Fatalf("dry-run out=%q want %q", out, want)
	}
}

func TestCopyVerboseReportsChunksAndSizes(t *testing.T) {
	env := map[string]string{"STY": "1.pts-0.h", "TERM": "screen"}
	code, _, errOut := harness{env: env, stdin: strings.Repeat("x", 600)}.run(t,
		"copy", "-verbose", "-out", "-")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"payload=600", "mux=screen", "chunks=4"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("verbose line missing %q: %q", want, errOut)
		}
	}
	// screen outermost around tmux: screen envelopes the tmux-wrapped
	// bytes, so the count must reflect that size, not the bare sequence.
	// 756 bytes → a 1016-byte sequence (4 chunks bare) that tmux wrapping
	// pushes to 1026 bytes → 5 chunks.
	code, _, errOut = harness{env: plainEnv(), stdin: strings.Repeat("x", 756)}.run(t,
		"copy", "-verbose", "-mux", "screen,tmux", "-out", "-")
	if code != ExitOK || !strings.Contains(errOut, "chunks=5") {
		t.Fatalf("nested chunk count wrong: code=%d %q", code, errOut)
	}
}

func TestCopyOutputDestinations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq.bin")
	code, _, _ := harness{env: plainEnv(), stdin: "file"}.run(t,
		"copy", "-mux", "none", "-out", path)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "\x1b]52;c;ZmlsZQ==\a" {
		t.Fatalf("file content %q err %v", b, err)
	}
	// -out auto without a controlling terminal (CI, tests) must not
	// fail; it degrades to stdout.
	code, out, _ := harness{env: plainEnv(), stdin: "x"}.run(t, "copy", "-mux", "none")
	if code != ExitOK || !strings.Contains(out, "]52;c;") {
		t.Fatalf("auto fallback: code=%d out=%q", code, out)
	}
}
