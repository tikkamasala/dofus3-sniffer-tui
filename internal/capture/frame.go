package capture

import "encoding/binary"

// Extract pulls as many complete VarInt-length-prefixed frames out of buf as
// it can, returning the payloads and whatever trailing bytes do not yet form
// a complete frame. The returned payload slices own their bytes (they are
// copied out of buf) so callers may reuse or discard buf after the call.
func Extract(buf []byte) (frames [][]byte, rest []byte) {
	for {
		size, n := binary.Uvarint(buf)
		if n <= 0 || uint64(len(buf)-n) < size {
			return frames, buf
		}
		end := uint64(n) + size
		payload := make([]byte, size)
		copy(payload, buf[n:end])
		frames = append(frames, payload)
		buf = buf[end:]
	}
}
