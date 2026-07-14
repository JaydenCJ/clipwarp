# OSC 52 and multiplexer passthrough: the wire formats clipwarp speaks

This document is the reference for every byte clipwarp emits or parses.
The formats are compatibility surfaces: changing any of them requires a
builder test, a parser test, and a round-trip test (see CONTRIBUTING.md).

## The OSC 52 sequence

OSC 52 is "Manipulate Selection Data" from xterm's ctlseqs. It is the
only clipboard mechanism that travels in-band — through SSH, serial
lines, and anything else that carries terminal bytes:

```
ESC ] 5 2 ; <targets> ; <payload> <terminator>
```

| Field | Values | Notes |
|---|---|---|
| `<targets>` | `c` `p` `q` `s` `0`–`7`, combinable | `c` = system clipboard, `p` = X11 primary, `s` = secondary, `q` = xterm cut-buffer shorthand, digits = numbered cut buffers. Nearly every terminal maps everything to the system clipboard. |
| `<payload>` | base64 | the clipboard bytes, standard base64 |
| `<payload>` | `?` | a **query**: the terminal answers with a regular write sequence carrying the current selection |
| `<payload>` | anything else | **clears** the selection (clipwarp uses `!`) |
| `<terminator>` | BEL (`0x07`) or ST (`ESC \`) | clipwarp emits BEL by default (`-st` switches); the parser also accepts the 8-bit ST `0x9C`, which some terminals use in replies |

Empty payload (`ESC ]52;c; BEL`) sets an empty selection — distinct from
a clear on some terminals, so clipwarp preserves the difference.

## tmux passthrough

tmux parses everything an application writes and swallows sequences it
does not forward. The escape hatch is the `tmux;` DCS envelope:

```
ESC P tmux ; <payload with every ESC doubled> ESC \
```

Every `ESC` byte in the payload is doubled so tmux's parser cannot
mistake payload bytes for the envelope's own terminator. tmux ≥ 3.3
additionally requires `set -g allow-passthrough on`. Independently,
`set -g set-clipboard on` makes tmux itself forward plain OSC 52 — with
it enabled you may not need wrapping at all, but the envelope is always
safe.

## screen passthrough and chunking

GNU screen forwards plain DCS envelopes (`ESC P … ESC \`) verbatim —
but it buffers the whole envelope first, and that buffer tops out around
**768 bytes**. A clipboard payload of a few kilobytes therefore cannot
travel in one envelope. The fix, used by every robust OSC 52 script and
formalized in clipwarp, is chunking:

```
ESC P <bytes 0..255> ESC \  ESC P <bytes 256..511> ESC \  …
```

screen strips each envelope and forwards the raw contents in order, so
the outer terminal sees the original escape sequence reassembled —
even though the chunk boundaries fall mid-sequence. clipwarp uses
256-byte chunks: generous headroom under the 768-byte buffer, with
envelope overhead below 2%.

## Nesting

For a shell inside tmux inside screen, tmux parses the bytes first, so
its envelope must be the **outermost** byte layer:

```
tmux(screen(osc52))   # process → tmux → screen → terminal
```

tmux strips its envelope and emits the screen envelope; screen strips
that and the terminal receives the bare OSC 52. The reverse nesting
composes the same way in the opposite order. `-mux` takes the chain
**innermost first** (`-mux tmux,screen` for the example above), and
auto-detection breaks the `TMUX`+`STY` ambiguity via `TERM`: the
innermost multiplexer is the one that set it.

Unwrapping nested streams offline (`clipwarp decode`, `wrap -undo`) has
one inherent ambiguity: a screen chunk's payload may itself contain
`ESC \` (the terminator of a nested tmux envelope). The resolver treats
`ESC \` as a chunk boundary only when followed by another `ESC P` or
end-of-input — exact for every stream clipwarp itself produces.

## Size limits

There is no in-band way to ask a terminal its OSC limit, so clipwarp
budgets conservatively: 100 000 bytes for unknown terminals (the classic
hterm cap that most OSC 52 tooling inherited), raised for terminals
known to accept more (kitty 8 MiB; WezTerm, Alacritty, foot, iTerm2,
Windows Terminal 1 MiB). The budget applies to the *sequence* (base64
inflates payloads by 4/3), and `-on-oversize` chooses the policy:
`error` (default), `truncate` (cut on a whole base64 quantum, warn on
stderr), or `force` (you know your terminal better than the table).

## The query round trip

`clipwarp paste` writes `ESC ]52;c;? BEL` (wrapped as needed), switches
the controlling terminal to raw mode, and reads until a complete OSC 52
sequence arrives or the deadline passes. Replies may dribble in across
several reads and may use any of the three terminators. Many terminals
deliberately refuse selection queries (reading a clipboard is a bigger
security surface than writing one) — clipwarp reports that honestly
instead of hanging, and `-stdin` decodes replies captured elsewhere.
