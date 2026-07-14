# clipwarp examples

Runnable material showing clipwarp in the situations it was built for.

- **remote-copy.sh** — the everyday loop: copy a command's output, a
  file, and a truncated log from a remote shell straight to the local
  clipboard, with a capability check first. Run it on any host you are
  SSH'd into (`bash remote-copy.sh`); it degrades gracefully when no
  terminal is attached.
- **tmux.conf** — the two tmux options that make OSC 52 work through
  tmux, with comments explaining what each one does and which tmux
  versions need them. Append to `~/.tmux.conf` or load with
  `tmux source-file examples/tmux.conf`.

Everything here is offline and side-effect-free apart from writing to
your terminal's clipboard.
