# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-13

### Added

- `clipwarp copy`: stdin or files to the clipboard via OSC 52, with
  selection targets (`c`, `p`, `s`, `q`, `0`–`7`, combinable), BEL or ST
  terminators, `-clear`, `-trim`, `-dry-run`, and delivery to the
  controlling terminal, stdout, or a file.
- Multiplexer passthrough wrapping: tmux `ESC Ptmux;` envelopes with
  ESC-doubling, GNU screen DCS chunking (≤256-byte envelopes, safely
  under screen's 768-byte DCS buffer), and nested chains in either
  order (`-mux tmux,screen`), auto-detected from `TMUX`/`STY`/`TERM`
  with a TERM-based tiebreak for double nesting.
- Size budgets with per-terminal limits and an explicit oversize policy
  (`-on-oversize error|truncate|force`); truncation keeps the sequence
  inside the budget on a whole base64 quantum, never mid-encoding.
- `clipwarp paste`: interactive OSC 52 query over `/dev/tty` in raw
  mode with a read deadline, plus `-stdin` for decoding captured
  replies offline; accepts BEL, 7-bit ST and 8-bit ST terminators.
- `clipwarp caps`: offline capability detection (kitty, WezTerm,
  Alacritty, foot, iTerm2, Apple Terminal, VTE ≥0.76 boundary, Konsole,
  Windows Terminal, xterm, st, rxvt, Linux console) with honest
  yes/probably/opt-in/no verdicts, `-json` output, and `-check` exit
  codes for scripts.
- `clipwarp wrap` / `clipwarp decode`: filter-mode envelope wrapping,
  best-effort unwrapping of any nesting order, and OSC 52 extraction
  from arbitrary byte streams with malformed-sequence warnings.
- Wire-format reference (`docs/osc52.md`) and runnable examples
  (`examples/remote-copy.sh`, `examples/tmux.conf`).
- 89 deterministic offline tests (sequence building, stream parsing,
  wrapping/unwrapping, detection, and in-process CLI runs against a
  scripted fake terminal) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/clipwarp/releases/tag/v0.1.0
