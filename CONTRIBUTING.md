# Contributing to clipwarp

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22 and a POSIX shell (for the smoke script); nothing else.

```bash
git clone https://github.com/JaydenCJ/clipwarp && cd clipwarp
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary and drives the real copy → wire →
decode loop through plain, tmux, screen and nested environments, plus
size budgets and every documented exit code; it must finish by printing
`SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (89 deterministic tests, no network, no sleeps).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (osc52, wrap, and detect never touch the OS — only
   `internal/tty` and the command layer do).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR. clipwarp never talks to the network. No telemetry.
- The emitted byte streams are a compatibility surface: changes to the
  OSC 52 framing, the tmux/screen envelopes, or the chunk size need a
  builder test, a parser test, and a round-trip test.
- The `caps -json` field names are also a compatibility surface —
  scripts key off them. Treat renames as breaking.
- Never make copy fail because detection is unsure: unknown terminals
  get the conservative default budget, not an error.
- Code comments and doc comments are written in English.
- Determinism first: everything except `internal/tty` is a pure
  function of bytes and an environment-lookup callback.

## Reporting bugs

Include the output of `clipwarp version` and `clipwarp caps` from the
affected shell, your terminal emulator and multiplexer versions, and —
for wire-format bugs — the exact bytes (`clipwarp copy -out trace.bin`,
then attach `trace.bin`), since any capture can be replayed offline with
`clipwarp decode < trace.bin`.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
