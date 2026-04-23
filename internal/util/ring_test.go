package util

import "testing"

func TestRing_BasicAppend(t *testing.T) {
	r := NewRing[int](3)
	if r.Len() != 0 {
		t.Fatal("empty ring should have Len 0")
	}
	r.Append(10)
	r.Append(20)
	if r.Len() != 2 {
		t.Fatalf("Len: %d", r.Len())
	}
	if r.At(0) != 10 || r.At(1) != 20 {
		t.Fatalf("At: %d %d", r.At(0), r.At(1))
	}
}

func TestRing_Overflow(t *testing.T) {
	r := NewRing[int](3)
	for i := 1; i <= 5; i++ {
		r.Append(i)
	}
	if r.Len() != 3 {
		t.Fatalf("Len after overflow: %d", r.Len())
	}
	want := []int{3, 4, 5}
	for i, w := range want {
		if r.At(i) != w {
			t.Fatalf("At(%d): want %d, got %d", i, w, r.At(i))
		}
	}
}

func TestRing_SeqTracking(t *testing.T) {
	r := NewRing[int](2)
	s0 := r.Append(100) // seq 0
	s1 := r.Append(200) // seq 1
	s2 := r.Append(300) // seq 2, evicts 100
	if s0 != 0 || s1 != 1 || s2 != 2 {
		t.Fatalf("seqs: %d %d %d", s0, s1, s2)
	}
	if r.OldestSeq() != 1 {
		t.Fatalf("oldest seq: %d", r.OldestSeq())
	}
	if r.At(0) != 200 || r.At(1) != 300 {
		t.Fatalf("ordering lost: %d %d", r.At(0), r.At(1))
	}
}

func TestRing_ZeroCapCoerces(t *testing.T) {
	r := NewRing[int](0)
	r.Append(1)
	r.Append(2)
	if r.Len() != 1 || r.At(0) != 2 {
		t.Fatalf("zero-cap should coerce to 1: len=%d val=%d", r.Len(), r.At(0))
	}
}
