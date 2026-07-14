#!/usr/bin/env bash
# End-to-end smoke test for clipwarp: builds the binary, then drives the
# real copy → wire-bytes → decode/paste loop through plain, tmux, screen
# and nested environments, plus size budgets, capability detection and
# every documented exit code. No network, idempotent, finishes in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/clipwarp"

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/clipwarp) || fail "go build failed"

echo "2. version matches manifest"
"$BIN" version | grep -qx "clipwarp 0.1.0" || fail "version mismatch"

echo "3. copy in a plain terminal emits a bare OSC 52 sequence"
OUT="$(printf 'hello' | env -i TERM=xterm-kitty "$BIN" copy -out -)"
[ "$OUT" = "$(printf '\033]52;c;aGVsbG8=\a')" ] || fail "plain sequence wrong"

echo "4. copy inside tmux wraps in a passthrough envelope"
OUT="$(printf 'hi' | env -i TMUX=/tmp/s,1,0 TERM=tmux-256color "$BIN" copy -out -)"
[ "$OUT" = "$(printf '\033Ptmux;\033\033]52;c;aGk=\a\033\\')" ] || fail "tmux envelope wrong"

echo "5. copy inside screen chunks a large payload into small envelopes"
head -c 5000 /dev/zero | tr '\0' 'x' > "$WORKDIR/big.txt"
env -i STY=1.pts-0.h TERM=screen-256color "$BIN" copy -verbose -out "$WORKDIR/wire.bin" "$WORKDIR/big.txt" 2> "$WORKDIR/verbose.txt"
grep -q "mux=screen" "$WORKDIR/verbose.txt" || fail "screen mux not detected"
grep -q "chunks=27" "$WORKDIR/verbose.txt" || fail "expected 27 chunks for 5000 bytes"

echo "6. the chunked wire bytes decode back to the exact payload"
"$BIN" decode < "$WORKDIR/wire.bin" > "$WORKDIR/roundtrip.txt" || fail "decode failed"
cmp -s "$WORKDIR/big.txt" "$WORKDIR/roundtrip.txt" || fail "chunked round trip not byte-identical"

echo "7. nested tmux-inside-screen survives copy → paste -stdin"
printf 'nested payload' \
  | env -i TMUX=s,1,0 STY=9.pts-1.h TERM=tmux-256color "$BIN" copy -out - \
  | "$BIN" paste -stdin > "$WORKDIR/nested.txt"
[ "$(cat "$WORKDIR/nested.txt")" = "nested payload" ] || fail "nested round trip broken"

echo "8. paste -stdin decodes a captured terminal reply"
REPLY="$(printf '\033]52;c;c2VjcmV0\a' | "$BIN" paste -stdin)"
[ "$REPLY" = "secret" ] || fail "paste -stdin wrong: $REPLY"

echo "9. size budget: error by default, truncate fits, force overrides"
if head -c 200 /dev/zero | "$BIN" copy -max-bytes 100 -out - >/dev/null 2>&1; then
  fail "oversize payload should exit 1"
fi
LEN="$(head -c 200 /dev/zero | "$BIN" copy -max-bytes 100 -on-oversize truncate -mux none -out - 2>/dev/null | wc -c)"
[ "$LEN" -le 100 ] || fail "truncated sequence is $LEN bytes, over budget"
LEN="$(head -c 200 /dev/zero | "$BIN" copy -max-bytes 100 -on-oversize force -mux none -out - 2>/dev/null | wc -c)"
[ "$LEN" -gt 100 ] || fail "force did not emit the full sequence"

echo "10. caps detects environment offline, exit codes honest"
# Capture before grepping: `caps | grep -q` would let grep close the pipe
# early and (with pipefail) turn caps's harmless SIGPIPE into a failure.
CAPS="$(env -i KITTY_WINDOW_ID=1 TERM=xterm-kitty "$BIN" caps -json)"
grep -q '"osc52": "yes"' <<< "$CAPS" || fail "kitty caps wrong"
CAPS="$(env -i TMUX=x TERM=tmux-256color SSH_TTY=/dev/pts/0 "$BIN" caps)"
grep -q "wrap needed    true" <<< "$CAPS" || fail "tmux caps wrong"
if env -i TERM_PROGRAM=Apple_Terminal TERM=xterm-256color "$BIN" caps -check >/dev/null; then
  fail "caps -check should exit 1 on Apple Terminal"
fi

echo "11. wrap/unwrap filter round-trips raw bytes"
printf 'raw \033 bytes' | "$BIN" wrap -mux tmux,screen | "$BIN" wrap -undo > "$WORKDIR/undo.bin"
[ "$(cat "$WORKDIR/undo.bin")" = "$(printf 'raw \033 bytes')" ] || fail "wrap -undo round trip broken"

echo "12. clear emits the spec's non-base64 clear payload"
OUT="$("$BIN" copy -clear -mux none -out -)"
[ "$OUT" = "$(printf '\033]52;c;!\a')" ] || fail "clear sequence wrong"

echo "13. usage errors exit 2"
set +e
"$BIN" frobnicate >/dev/null 2>&1; [ $? -eq 2 ] || fail "unknown command should exit 2"
printf x | "$BIN" copy -target z -out - >/dev/null 2>&1; [ $? -eq 2 ] || fail "bad target should exit 2"
printf x | "$BIN" copy -mux zellij -out - >/dev/null 2>&1; [ $? -eq 2 ] || fail "bad mux should exit 2"
set -e

echo "14. decode reports failure honestly"
set +e
printf 'no sequences here' | "$BIN" decode >/dev/null 2>&1; [ $? -eq 1 ] || fail "decode of nothing should exit 1"
set -e

echo "SMOKE OK"
