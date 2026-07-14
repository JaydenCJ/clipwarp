// Tests for OSC 52 sequence building: exact wire bytes, target
// validation, and the size arithmetic the truncate policy relies on.
package osc52

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetBuildsExactSequenceForBothTerminators(t *testing.T) {
	if got, want := Set("c", []byte("hello"), TermBEL), []byte("\x1b]52;c;aGVsbG8=\a"); !bytes.Equal(got, want) {
		t.Fatalf("Set(BEL) = %q, want %q", got, want)
	}
	if got, want := Set("c", []byte("hello"), TermST), []byte("\x1b]52;c;aGVsbG8=\x1b\\"); !bytes.Equal(got, want) {
		t.Fatalf("Set(ST) = %q, want %q", got, want)
	}
}

func TestSetEmptyDataAndMultiTargetFraming(t *testing.T) {
	// Empty data is a legal "set the clipboard to nothing" — distinct
	// from Clear, which some terminals treat differently.
	if got, want := Set("c", nil, TermBEL), []byte("\x1b]52;c;\a"); !bytes.Equal(got, want) {
		t.Fatalf("Set(nil) = %q, want %q", got, want)
	}
	if got := Set("pc", []byte("x"), TermBEL); !bytes.HasPrefix(got, []byte("\x1b]52;pc;")) {
		t.Fatalf("multi-target prefix wrong: %q", got)
	}
}

func TestSetEncodesArbitraryBinaryData(t *testing.T) {
	// Clipboard payloads are not always text; NUL, ESC and BEL bytes in
	// the data must survive base64 without corrupting the sequence.
	data := []byte{0x00, 0x1b, 0x07, 0xff, 0xfe}
	got := Set("c", data, TermBEL)
	if bytes.ContainsAny(got[len("\x1b]52;c;"):len(got)-1], "\x00\xff") {
		t.Fatalf("raw bytes leaked into payload: %q", got)
	}
	seqs, _ := Scan(got)
	if len(seqs) != 1 || !bytes.Equal(seqs[0].Data, data) {
		t.Fatalf("round trip failed: %+v", seqs)
	}
}

func TestClearUsesNonBase64Payload(t *testing.T) {
	got := Clear("c", TermBEL)
	want := []byte("\x1b]52;c;!\a")
	if !bytes.Equal(got, want) {
		t.Fatalf("Clear = %q, want %q", got, want)
	}
}

func TestQueryUsesQuestionMark(t *testing.T) {
	got := Query("p", TermST)
	want := []byte("\x1b]52;p;?\x1b\\")
	if !bytes.Equal(got, want) {
		t.Fatalf("Query = %q, want %q", got, want)
	}
}

func TestValidateTargetAcceptsAllDocumentedCharacters(t *testing.T) {
	for _, c := range ValidTargets {
		if err := ValidateTarget(string(c)); err != nil {
			t.Errorf("ValidateTarget(%q) = %v, want nil", c, err)
		}
	}
	if err := ValidateTarget("cp0"); err != nil {
		t.Errorf("combination rejected: %v", err)
	}
}

func TestValidateTargetRejectsBadInput(t *testing.T) {
	if err := ValidateTarget(""); err == nil {
		t.Fatal("empty target accepted; a blank selection silently no-ops")
	}
	for _, bad := range []string{"x", "c;", "8", "C", "c x"} {
		if err := ValidateTarget(bad); err == nil {
			t.Errorf("ValidateTarget(%q) accepted", bad)
		}
	}
}

func TestEncodedLenMatchesRealSequenceLength(t *testing.T) {
	// The oversize check runs before encoding; if this arithmetic ever
	// drifts from Set's real output, budgets are enforced wrongly.
	for _, n := range []int{0, 1, 2, 3, 4, 100, 999} {
		data := bytes.Repeat([]byte{'a'}, n)
		if got, want := EncodedLen("c", n), len(Set("c", data, TermBEL)); got != want {
			t.Errorf("EncodedLen(c, %d) = %d, real sequence is %d", n, got, want)
		}
	}
	if EncodedLen("pc0", 3) != EncodedLen("c", 3)+2 {
		t.Fatal("target width not reflected in EncodedLen")
	}
}

func TestMaxDataLenFitsBudgetExactly(t *testing.T) {
	// The largest payload MaxDataLen allows must fit, and one more
	// base64 quantum must not — otherwise truncate over- or
	// under-shoots the terminal's limit.
	for _, limit := range []int{12, 50, 100, 1000} {
		keep := MaxDataLen("c", limit)
		if keep > 0 {
			if got := EncodedLen("c", keep); got > limit {
				t.Errorf("limit %d: kept %d bytes → %d-byte sequence overflows", limit, keep, got)
			}
		}
		if got := EncodedLen("c", keep+3); got <= limit {
			t.Errorf("limit %d: MaxDataLen=%d not maximal (%d more bytes still fit)", limit, keep, got)
		}
	}
	if got := MaxDataLen("c", 8); got != 0 {
		t.Fatalf("MaxDataLen with tiny budget = %d, want 0", got)
	}
}

func TestSetLargePayloadStaysWellFormed(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 20000) // 200 KB
	seq := Set("c", data, TermBEL)
	if !strings.HasPrefix(string(seq), "\x1b]52;c;") || seq[len(seq)-1] != BEL {
		t.Fatal("large sequence framing broken")
	}
	seqs, malformed := Scan(seq)
	if malformed != 0 || len(seqs) != 1 || !bytes.Equal(seqs[0].Data, data) {
		t.Fatal("large payload did not round-trip")
	}
}
