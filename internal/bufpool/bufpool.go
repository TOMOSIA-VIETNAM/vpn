// Package bufpool recycles the fixed-size packet buffers of the data plane.
// Every tunnelled packet used to allocate (and later garbage-collect) two or
// three slices; at line rate that is tens of thousands of short-lived
// allocations a second, which keeps the GC busy and the resident heap well
// above what the live data needs.
package bufpool

import "sync"

// Size fits any packet of a 1280-1400 MTU tunnel plus ESP/L2TP/PPP framing
// (worst case with a 3DES/AES block of padding and a 32-byte SHA-512 ICV).
const Size = 2048

// The pool holds *[Size]byte rather than *[]byte so that Put neither
// allocates a slice header nor lets one escape.
var pool = sync.Pool{New: func() any { return new([Size]byte) }}

// Get returns a buffer of length n, from the pool when n <= Size, otherwise a
// fresh slice. Its contents are undefined.
func Get(n int) []byte {
	if n > Size {
		return make([]byte, n)
	}
	return pool.Get().(*[Size]byte)[:n]
}

// Put hands b back. Only buffers that came from Get with a pooled capacity are
// kept; anything else is left to the garbage collector, so calling Put on a
// slice of unknown origin is safe. The caller must not touch b afterwards.
func Put(b []byte) {
	if cap(b) != Size {
		return
	}
	pool.Put((*[Size]byte)(b[:Size]))
}
