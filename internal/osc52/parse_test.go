// Tests for OSC 52 stream parsing: real terminal replies are embedded in
// noise, use either terminator, and are sometimes cut off mid-sequence.
package osc52

import (
	"bytes"
	"testing"
)

func TestScanAcceptsEveryTerminatorVariant(t *testing.T) {
	// Terminals answer with BEL, 7-bit ST, or (rarely) the single-byte
	// 0x9C ST; rejecting any of them would break paste against real
	// hardware.
	cases := []struct {
		name   string
		stream []byte
		want   string
	}{
		{"BEL", []byte("\x1b]52;c;aGVsbG8=\a"), "hello"},
		{"7-bit ST", []byte("\x1b]52;c;d29ybGQ=\x1b\\"), "world"},
		{"8-bit ST", append([]byte("\x1b]52;c;aGk="), 0x9c), "hi"},
	}
	for _, c := range cases {
		seqs, malformed := Scan(c.stream)
		if malformed != 0 || len(seqs) != 1 {
			t.Errorf("%s: %d seqs, %d malformed", c.name, len(seqs), malformed)
			continue
		}
		s := seqs[0]
		if s.Target != "c" || s.Kind != KindWrite || string(s.Data) != c.want {
			t.Errorf("%s: parsed %+v", c.name, s)
		}
	}
}

func TestScanIgnoresSurroundingTerminalNoise(t *testing.T) {
	stream := []byte("prompt$ \x1b[1;32mgreen\x1b[0m\x1b]52;c;eA==\a\r\nmore output")
	seqs, malformed := Scan(stream)
	if malformed != 0 || len(seqs) != 1 || string(seqs[0].Data) != "x" {
		t.Fatalf("noise handling failed: %+v (%d malformed)", seqs, malformed)
	}
}

func TestScanMultipleSequencesInOrder(t *testing.T) {
	stream := []byte("\x1b]52;c;YQ==\agap\x1b]52;p;Yg==\x1b\\")
	seqs, _ := Scan(stream)
	if len(seqs) != 2 || string(seqs[0].Data) != "a" || string(seqs[1].Data) != "b" {
		t.Fatalf("order or count wrong: %+v", seqs)
	}
	if seqs[1].Target != "p" {
		t.Fatalf("second target = %q", seqs[1].Target)
	}
}

func TestScanClassifiesQuery(t *testing.T) {
	seqs, _ := Scan([]byte("\x1b]52;c;?\a"))
	if len(seqs) != 1 || seqs[0].Kind != KindQuery || seqs[0].Data != nil {
		t.Fatalf("query misclassified: %+v", seqs)
	}
}

func TestScanClassifiesClearOnInvalidBase64(t *testing.T) {
	// xterm semantics: non-base64 payload clears the selection. The
	// parser must mirror that, not error out.
	seqs, malformed := Scan([]byte("\x1b]52;c;!\a"))
	if malformed != 0 || len(seqs) != 1 || seqs[0].Kind != KindClear {
		t.Fatalf("clear misclassified: %+v (%d malformed)", seqs, malformed)
	}
}

func TestScanEmptyPayloadIsEmptyWrite(t *testing.T) {
	seqs, _ := Scan([]byte("\x1b]52;c;\a"))
	if len(seqs) != 1 || seqs[0].Kind != KindWrite || len(seqs[0].Data) != 0 {
		t.Fatalf("empty payload misparsed: %+v", seqs)
	}
}

func TestScanCountsBrokenSequencesAsMalformed(t *testing.T) {
	// A reply cut off by a dropped connection, or a body without the
	// target separator, must not be silently decoded as partial data.
	for _, stream := range []string{"\x1b]52;c;aGVsbG8=", "\x1b]52;nosemicolon\a"} {
		seqs, malformed := Scan([]byte(stream))
		if len(seqs) != 0 || malformed != 1 {
			t.Errorf("%q: %d seqs, %d malformed", stream, len(seqs), malformed)
		}
	}
}

func TestScanRecoversAfterMalformedSequence(t *testing.T) {
	stream := []byte("\x1b]52;bad!target;YQ==\a\x1b]52;c;Yg==\a")
	seqs, malformed := Scan(stream)
	if malformed != 1 || len(seqs) != 1 || string(seqs[0].Data) != "b" {
		t.Fatalf("recovery failed: %+v (%d malformed)", seqs, malformed)
	}
}

func TestScanAbortsSequenceOnStrayESC(t *testing.T) {
	// A bare ESC inside an OSC body aborts it on real terminals; the
	// parser must not swallow the following legitimate sequence.
	stream := []byte("\x1b]52;c;aG\x1b[31m\x1b]52;p;eQ==\a")
	seqs, malformed := Scan(stream)
	if len(seqs) != 1 || string(seqs[0].Data) != "y" || malformed == 0 {
		t.Fatalf("stray ESC handling: %+v (%d malformed)", seqs, malformed)
	}
}

func TestCompleteTracksAnAssemblingReply(t *testing.T) {
	// Simulates the interactive read loop receiving the reply in two
	// chunks — the exact scenario Complete exists for.
	if Complete([]byte("just a prompt \x1b[1m bold")) {
		t.Fatal("noise reported complete")
	}
	buf := []byte("\x1b]52;c;aGVs")
	if Complete(buf) {
		t.Fatal("partial reply reported complete; paste would truncate")
	}
	buf = append(buf, []byte("bG8=\a")...)
	if !Complete(buf) {
		t.Fatal("assembled reply not detected as complete")
	}
}

func TestFirstWriteSkipsQueriesAndClears(t *testing.T) {
	stream := []byte("\x1b]52;c;?\a\x1b]52;c;!\a\x1b]52;c;cGF5bG9hZA==\a")
	seq, ok := FirstWrite(stream)
	if !ok || string(seq.Data) != "payload" {
		t.Fatalf("FirstWrite = %+v, %v", seq, ok)
	}
}

func TestFirstWriteFalseWhenNoWriteExists(t *testing.T) {
	if _, ok := FirstWrite([]byte("\x1b]52;c;?\a")); ok {
		t.Fatal("query alone should not satisfy FirstWrite")
	}
}

func TestScanHugeSequenceRoundTrips(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, 300000)
	stream := Set("c", data, TermST)
	seqs, malformed := Scan(stream)
	if malformed != 0 || len(seqs) != 1 || !bytes.Equal(seqs[0].Data, data) {
		t.Fatal("300 KB payload did not survive scan")
	}
}
