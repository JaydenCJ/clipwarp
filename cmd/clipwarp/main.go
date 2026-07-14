// Command clipwarp copies and pastes through SSH, tmux and screen using
// OSC 52 escape sequences. All logic lives in internal packages; main only
// wires the real process environment and exit code.
package main

import (
	"os"

	"github.com/JaydenCJ/clipwarp/internal/cli"
)

func main() {
	os.Exit(cli.Run(cli.NewApp(), os.Args[1:]))
}
