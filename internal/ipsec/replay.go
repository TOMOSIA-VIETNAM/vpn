package ipsec

import "fmt"

// replayWords sizes the anti-replay bitmap: 512 64-bit words (4 KiB).
const replayWords = 512

// ReplayWindowSize is how far behind the highest sequence number seen a
// packet may still arrive and be accepted: 32,704 packets (~45 MB of
// 1400-byte packets in flight), wide enough for the reordering of a busy
// tunnel. One word of the ring is always the one being filled, so the usable
// window is a word short of the bitmap (RFC 6479 §2).
const ReplayWindowSize = (replayWords - 1) * 64

// replayWindow is RFC 6479's ring-bitmap anti-replay window (RFC 4303
// §3.4.3). Accepting a packet costs O(1) whatever the window size: moving
// forward clears only the words it skips, instead of shifting the whole
// window (which, for a window this wide, meant copying megabytes per packet).
type replayWindow struct {
	top    uint32 // highest sequence number accepted so far
	init   bool   // top is valid: at least one packet was accepted
	bitmap [replayWords]uint64
}

// check reports whether seq may be accepted. It changes nothing: only a
// packet whose ICV verified may move the window (accept), or a forged one
// could push it past every genuine sequence number.
func (w *replayWindow) check(seq uint32) error {
	if seq == 0 {
		return fmt.Errorf("ESP replay check: sequence number 0 is invalid")
	}
	if !w.init || seq > w.top {
		return nil
	}
	if w.top-seq >= ReplayWindowSize {
		return fmt.Errorf("ESP replay check: sequence %d too old (highest seen %d)", seq, w.top)
	}
	if w.bitmap[wordOf(seq)]&bitOf(seq) != 0 {
		return fmt.Errorf("ESP replay check: sequence %d already seen (replay)", seq)
	}
	return nil
}

// accept records seq, which check allowed and whose ICV verified.
func (w *replayWindow) accept(seq uint32) {
	switch {
	case !w.init:
		w.init = true
		w.top = seq
	case seq > w.top:
		// Clear every word the window moves over, the word seq lands in
		// included; at most the whole ring.
		from, to := w.top>>6, seq>>6
		n := to - from
		if n > replayWords {
			n = replayWords
		}
		for i := uint32(1); i <= n; i++ {
			w.bitmap[(from+i)%replayWords] = 0
		}
		w.top = seq
	}
	w.bitmap[wordOf(seq)] |= bitOf(seq)
}

func wordOf(seq uint32) uint32 { return (seq >> 6) % replayWords }
func bitOf(seq uint32) uint64  { return 1 << (seq & 63) }
