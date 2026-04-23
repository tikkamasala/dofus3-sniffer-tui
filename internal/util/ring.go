package util

// Ring is a fixed-capacity circular buffer. When full, Append overwrites the
// oldest element. Indexing is stable by monotonic sequence number (Seq), so
// callers can store references to items even as the ring rolls over.
type Ring[T any] struct {
	buf   []T
	start int
	size  int
	seq   uint64
}

func NewRing[T any](cap int) *Ring[T] {
	if cap < 1 {
		cap = 1
	}
	return &Ring[T]{buf: make([]T, cap)}
}

func (r *Ring[T]) Cap() int  { return len(r.buf) }
func (r *Ring[T]) Len() int  { return r.size }
func (r *Ring[T]) Seq() uint64 { return r.seq }

// Append returns the sequence number assigned to the new item.
func (r *Ring[T]) Append(v T) uint64 {
	seq := r.seq
	r.seq++
	if r.size < len(r.buf) {
		r.buf[(r.start+r.size)%len(r.buf)] = v
		r.size++
		return seq
	}
	r.buf[r.start] = v
	r.start = (r.start + 1) % len(r.buf)
	return seq
}

// At returns the item at logical position i (0 = oldest).
func (r *Ring[T]) At(i int) T {
	return r.buf[(r.start+i)%len(r.buf)]
}

// OldestSeq is the sequence number of index 0 (or of the next append if empty).
func (r *Ring[T]) OldestSeq() uint64 {
	return r.seq - uint64(r.size)
}
