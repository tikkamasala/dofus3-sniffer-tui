package capture

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
)

// varintPrefix encodes n as protobuf VarInt. Same encoding used by binary.Uvarint.
func varintPrefix(n uint64) []byte {
	b := make([]byte, binary.MaxVarintLen64)
	w := binary.PutUvarint(b, n)
	return b[:w]
}

func framed(payloads ...[]byte) []byte {
	var out []byte
	for _, p := range payloads {
		out = append(out, varintPrefix(uint64(len(p)))...)
		out = append(out, p...)
	}
	return out
}

func TestExtract_SingleFrame(t *testing.T) {
	p := []byte("hello-world")
	buf := framed(p)
	frames, rest := Extract(buf)
	if len(frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(frames))
	}
	if !bytes.Equal(frames[0], p) {
		t.Fatalf("frame mismatch: %q vs %q", frames[0], p)
	}
	if len(rest) != 0 {
		t.Fatalf("want empty rest, got %d bytes", len(rest))
	}
}

func TestExtract_Multiple(t *testing.T) {
	a := []byte("aaa")
	b := bytes.Repeat([]byte("B"), 200)
	c := []byte("c")
	buf := framed(a, b, c)
	frames, rest := Extract(buf)
	if len(frames) != 3 {
		t.Fatalf("want 3 frames, got %d", len(frames))
	}
	if !bytes.Equal(frames[0], a) || !bytes.Equal(frames[1], b) || !bytes.Equal(frames[2], c) {
		t.Fatal("frame contents mismatch")
	}
	if len(rest) != 0 {
		t.Fatalf("want empty rest, got %d", len(rest))
	}
}

func TestExtract_PartialTrailing(t *testing.T) {
	a := []byte("alpha")
	buf := framed(a)
	buf = append(buf, varintPrefix(10)...) // length prefix for 10 bytes we don't have
	buf = append(buf, 1, 2, 3)             // only 3 of 10
	frames, rest := Extract(buf)
	if len(frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(frames))
	}
	// Rest must contain the unfinished frame (length prefix + partial).
	if len(rest) != len(varintPrefix(10))+3 {
		t.Fatalf("rest size wrong: %d", len(rest))
	}
}

func TestExtract_ByteByByteFeed(t *testing.T) {
	// Simulate a slow reassembler handing us one byte at a time. We must
	// emit exactly the originally-framed payloads once all bytes are fed.
	orig := [][]byte{
		[]byte("one"),
		bytes.Repeat([]byte("x"), 500),
		[]byte(""),           // zero-length frame is legal
		[]byte("lastFrame"),
	}
	src := framed(orig...)

	var buf []byte
	var got [][]byte
	for _, b := range src {
		buf = append(buf, b)
		frames, rest := Extract(buf)
		buf = rest
		got = append(got, frames...)
	}
	if len(buf) != 0 {
		t.Fatalf("leftover bytes after full feed: %d", len(buf))
	}
	if len(got) != len(orig) {
		t.Fatalf("frame count: want %d, got %d", len(orig), len(got))
	}
	for i := range orig {
		if !bytes.Equal(got[i], orig[i]) {
			t.Fatalf("frame %d mismatch", i)
		}
	}
}

func TestExtract_RandomChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	var orig [][]byte
	for i := 0; i < 20; i++ {
		n := rng.Intn(600) + 1
		p := make([]byte, n)
		rng.Read(p)
		orig = append(orig, p)
	}
	src := framed(orig...)

	var buf []byte
	var got [][]byte
	i := 0
	for i < len(src) {
		chunk := rng.Intn(250) + 1
		if i+chunk > len(src) {
			chunk = len(src) - i
		}
		buf = append(buf, src[i:i+chunk]...)
		frames, rest := Extract(buf)
		buf = rest
		got = append(got, frames...)
		i += chunk
	}
	if len(buf) != 0 {
		t.Fatalf("leftover: %d", len(buf))
	}
	if len(got) != len(orig) {
		t.Fatalf("count: want %d, got %d", len(orig), len(got))
	}
	for i := range orig {
		if !bytes.Equal(got[i], orig[i]) {
			t.Fatalf("frame %d content mismatch", i)
		}
	}
}
