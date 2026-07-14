// Package cli implements the clipwarp command-line interface. Everything
// is driven through App so tests can inject stdin/stdout/stderr, a fake
// environment and a fake terminal, and run the whole binary in-process.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaydenCJ/clipwarp/internal/detect"
	"github.com/JaydenCJ/clipwarp/internal/tty"
	"github.com/JaydenCJ/clipwarp/internal/version"
	"github.com/JaydenCJ/clipwarp/internal/wrap"
)

// Exit codes, stable for scripting.
const (
	ExitOK      = 0 // success
	ExitRuntime = 1 // payload too large, nothing to decode, unsupported terminal with --check, I/O failure
	ExitUsage   = 2 // bad flags, unknown command, invalid target
)

// App carries every ambient dependency the commands touch.
type App struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Look    func(string) (string, bool) // environment lookup
	OpenTTY func() (tty.Terminal, error)
}

// NewApp wires the real process environment.
func NewApp() *App {
	return &App{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Look:    os.LookupEnv,
		OpenTTY: tty.Open,
	}
}

const usageText = `clipwarp — copy and paste through SSH, tmux and screen using OSC 52

Usage:
  clipwarp copy [flags] [file...]   copy stdin or files to the clipboard
  clipwarp paste [flags]            paste the clipboard to stdout
  clipwarp caps [flags]             show detected terminal capabilities
  clipwarp wrap [flags]             wrap raw stdin bytes for tmux/screen
  clipwarp decode [flags]           decode OSC 52 sequences from stdin
  clipwarp version                  print the version

Run 'clipwarp <command> -h' for command flags.
Exit codes: 0 ok, 1 runtime failure, 2 usage error.
`

// Run dispatches a full argv (without the program name) and returns the
// process exit code.
func Run(a *App, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stderr, usageText)
		return ExitUsage
	}
	switch args[0] {
	case "copy":
		return cmdCopy(a, args[1:])
	case "paste":
		return cmdPaste(a, args[1:])
	case "caps":
		return cmdCaps(a, args[1:])
	case "wrap":
		return cmdWrap(a, args[1:])
	case "decode":
		return cmdDecode(a, args[1:])
	case "version", "--version", "-V":
		fmt.Fprintf(a.Stdout, "clipwarp %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		fmt.Fprint(a.Stdout, usageText)
		return ExitOK
	}
	fmt.Fprintf(a.Stderr, "clipwarp: unknown command %q\n\n%s", args[0], usageText)
	return ExitUsage
}

// newFlagSet builds a flag set that reports usage errors on the app's
// stderr and never os.Exits.
func newFlagSet(a *App, name string) *flag.FlagSet {
	fs := flag.NewFlagSet("clipwarp "+name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	return fs
}

// muxChain resolves the --mux flag: "auto" defers to environment
// detection, anything else is an explicit innermost-first chain.
func muxChain(a *App, muxFlag string) ([]wrap.Mux, error) {
	if muxFlag == "auto" {
		return detect.Muxes(a.Look), nil
	}
	return wrap.ParseChain(muxFlag)
}

// fail prints a runtime error in the conventional prefix format.
func fail(a *App, format string, args ...any) int {
	fmt.Fprintf(a.Stderr, "clipwarp: "+format+"\n", args...)
	return ExitRuntime
}

// plural renders "1 envelope layer" / "3 envelope layers" for messages.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// escapeVisible renders control bytes readably for --dry-run: ESC as \e,
// BEL as \a, everything else printable stays literal, the rest is \xNN.
func escapeVisible(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0x1b:
			sb.WriteString(`\e`)
		case c == 0x07:
			sb.WriteString(`\a`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, `\x%02x`, c)
		}
	}
	return sb.String()
}
