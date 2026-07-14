#!/usr/bin/env bash
# Everyday clipwarp usage from a remote shell (SSH, tmux, screen — any
# combination). Run: bash remote-copy.sh
set -euo pipefail

# 0. Is this terminal likely to accept OSC 52 at all? -check exits 1
#    only for known-unsupported terminals, so unknown setups still try.
if ! clipwarp caps -check >/dev/null 2>&1; then
  echo "this terminal is known to lack OSC 52 — see 'clipwarp caps'" >&2
  exit 1
fi
clipwarp caps

# 1. Copy a command's output. -trim drops the trailing newline so a
#    paste into a form field doesn't submit it.
hostname | clipwarp copy -trim
echo "hostname copied — paste it locally"

# 2. Copy a file. Multiple files concatenate in order, like cat.
clipwarp copy /etc/hostname

# 3. Copy the tail of a big log, truncating politely if the terminal's
#    budget is smaller than the log.
tail -c 200000 /var/log/syslog 2>/dev/null \
  | clipwarp copy -on-oversize truncate \
  || echo "no readable syslog on this host; skipping" >&2

# 4. See exactly what would hit the wire, without touching the
#    clipboard (great for debugging a multiplexer setup).
printf 'debug me' | clipwarp copy -dry-run -verbose

# 5. Paste back from the local clipboard, if the terminal answers
#    queries (many only implement copy — the error says so).
clipwarp paste -newline || true
