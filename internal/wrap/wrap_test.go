// Tests for multiplexer wrapping: exact envelope bytes, chunk boundaries,
// nesting order, and round-tripping through Unwrap.
package wrap

import (
	"bytes"
	"testing"
)

func TestTmuxWrapEnvelopesAndDoublesESC(t *testing.T) {
	got := TmuxWrap([]byte("\x1b]52;c;eA==\a"))
	want := []byte("\x1bPtmux;\x1b\x1b]52;c;eA==\a\x1b\\")
	if !bytes.Equal(got, want) {
		t.Fatalf("TmuxWrap = %q, want %q", got, want)
	}
	if got := TmuxWrap(nil); !bytes.Equal(got, []byte("\x1bPtmux;\x1b\\")) {
		t.Fatalf("empty tmux envelope = %q", got)
	}
}

func TestTmuxWrapDoublesEveryESCNotJustTheFirst(t *testing.T) {
	// An ST-terminated inner sequence contains two ESCs; missing one
	// would make tmux end the envelope early and leak half the payload.
	inner := []byte("\x1b]52;c;eA==\x1b\\")
	got := TmuxWrap(inner)
	if n := bytes.Count(got, []byte{0x1b, 0x1b}); n != 2 {
		t.Fatalf("doubled-ESC count = %d, want 2 in %q", n, got)
	}
}

func TestScreenWrapSingleChunkUnderLimit(t *testing.T) {
	got := ScreenWrap([]byte("abc"), 0)
	want := []byte("\x1bPabc\x1b\\")
	if !bytes.Equal(got, want) {
		t.Fatalf("ScreenWrap = %q, want %q", got, want)
	}
	if got := ScreenWrap(nil, 0); len(got) != 0 {
		t.Fatalf("empty input produced %q", got)
	}
}

func TestScreenWrapSplitsAtExactChunkBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte{'x'}, ScreenChunk)
	if n := bytes.Count(ScreenWrap(payload, 0), []byte("\x1bP")); n != 1 {
		t.Fatalf("payload == chunk size produced %d envelopes, want 1", n)
	}
	payload = append(payload, 'y')
	got := ScreenWrap(payload, 0)
	if n := bytes.Count(got, []byte("\x1bP")); n != 2 {
		t.Fatalf("payload of chunk+1 produced %d envelopes, want 2", n)
	}
	// The final envelope must carry exactly the overflow byte.
	if !bytes.HasSuffix(got, []byte("\x1bPy\x1b\\")) {
		t.Fatalf("last envelope wrong: %q", got[len(got)-8:])
	}
}

func TestScreenWrapCustomChunkSize(t *testing.T) {
	got := ScreenWrap([]byte("abcdef"), 2)
	want := []byte("\x1bPab\x1b\\\x1bPcd\x1b\\\x1bPef\x1b\\")
	if !bytes.Equal(got, want) {
		t.Fatalf("chunked = %q, want %q", got, want)
	}
}

func TestScreenWrapEveryEnvelopeStaysUnderScreenDCSBuffer(t *testing.T) {
	// screen drops DCS content beyond ~768 bytes; each envelope
	// (opener + payload + terminator) must stay safely below that.
	out := ScreenWrap(bytes.Repeat([]byte{'z'}, 10000), 0)
	for _, env := range bytes.SplitAfter(out, []byte("\x1b\\")) {
		if len(env) > 768 {
			t.Fatalf("envelope of %d bytes would overflow screen's DCS buffer", len(env))
		}
	}
}

func TestChainOrdersInnermostEnvelopeOutermost(t *testing.T) {
	// Shell inside tmux inside screen: tmux parses the bytes first, so
	// the tmux envelope must be the outermost byte layer.
	osc := []byte("\x1b]52;c;eA==\a")
	got := Chain([]Mux{Tmux, Screen}, osc)
	want := TmuxWrap(ScreenWrap(osc, 0))
	if !bytes.Equal(got, want) {
		t.Fatalf("chain [tmux,screen] = %q, want %q", got, want)
	}
	if !bytes.HasPrefix(got, []byte("\x1bPtmux;")) {
		t.Fatal("tmux envelope is not the outermost layer")
	}
	if got := Chain(nil, osc); !bytes.Equal(got, osc) {
		t.Fatalf("empty chain modified bytes: %q", got)
	}
}

func TestChainReverseNesting(t *testing.T) {
	osc := []byte("\x1b]52;c;eA==\a")
	got := Chain([]Mux{Screen, Tmux}, osc)
	want := ScreenWrap(TmuxWrap(osc), 0)
	if !bytes.Equal(got, want) {
		t.Fatalf("chain [screen,tmux] = %q, want %q", got, want)
	}
}

func TestUnwrapTmuxRoundTrip(t *testing.T) {
	inner := []byte("\x1b]52;c;aGVsbG8=\x1b\\") // ST inside stresses un-doubling
	out, layers := Unwrap(TmuxWrap(inner))
	if layers != 1 || !bytes.Equal(out, inner) {
		t.Fatalf("tmux round trip: layers=%d out=%q", layers, out)
	}
}

func TestUnwrapScreenRoundTripAcrossChunks(t *testing.T) {
	inner := bytes.Repeat([]byte("payload-"), 200) // forces many chunks
	out, layers := Unwrap(ScreenWrap(inner, 0))
	if layers != 1 || !bytes.Equal(out, inner) {
		t.Fatalf("screen round trip: layers=%d len=%d want %d", layers, len(out), len(inner))
	}
}

func TestUnwrapNestedEnvelopesBothOrders(t *testing.T) {
	// chain [screen, tmux]: the screen chunks contain a raw ESC \ from
	// the tmux envelope terminator — the ambiguous case the resolver
	// heuristic exists for. The reverse order must work too.
	osc := []byte("\x1b]52;c;aGVsbG8=\a")
	for _, chain := range [][]Mux{{Screen, Tmux}, {Tmux, Screen}} {
		out, layers := Unwrap(Chain(chain, osc))
		if layers != 2 || !bytes.Equal(out, osc) {
			t.Fatalf("chain %s: layers=%d out=%q", ChainString(chain), layers, out)
		}
	}
}

func TestUnwrapChunkBoundarySplittingEscapeSequence(t *testing.T) {
	// A 2-byte chunk size guarantees the inner ESC bytes land on
	// envelope boundaries; reassembly must still be byte-exact.
	inner := []byte("\x1b]52;c;eXo=\a")
	out, _ := Unwrap(ScreenWrap(inner, 2))
	if !bytes.Equal(out, inner) {
		t.Fatalf("split-escape reassembly failed: %q", out)
	}
}

func TestUnwrapPreservesBytesOutsideEnvelopes(t *testing.T) {
	plain := []byte("no envelopes here \x1b[1mjust sgr\x1b[0m")
	out, layers := Unwrap(plain)
	if layers != 0 || !bytes.Equal(out, plain) {
		t.Fatalf("plain bytes altered: layers=%d out=%q", layers, out)
	}
	stream := append([]byte("before "), TmuxWrap([]byte("X"))...)
	stream = append(stream, []byte(" after")...)
	out, _ = Unwrap(stream)
	if string(out) != "before X after" {
		t.Fatalf("surrounding bytes lost: %q", out)
	}
}

func TestParseChainVariants(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"none", "none", false},
		{"", "none", false},
		{"tmux", "tmux", false},
		{"screen", "screen", false},
		{"tmux,screen", "tmux,screen", false},
		{"screen, tmux", "screen,tmux", false}, // spaces tolerated
		{"zellij", "", true},
		{"none,tmux", "", true}, // none cannot be combined
	}
	for _, c := range cases {
		chain, err := ParseChain(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseChain(%q) accepted", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseChain(%q) = %v", c.in, err)
			continue
		}
		if got := ChainString(chain); got != c.want {
			t.Errorf("ParseChain(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestChunkCount(t *testing.T) {
	cases := []struct{ n, chunk, want int }{
		{0, 256, 0},
		{1, 256, 1},
		{256, 256, 1},
		{257, 256, 2},
		{1000, 0, 4}, // 0 means the default of 256
	}
	for _, c := range cases {
		if got := ChunkCount(c.n, c.chunk); got != c.want {
			t.Errorf("ChunkCount(%d, %d) = %d, want %d", c.n, c.chunk, got, c.want)
		}
	}
}
