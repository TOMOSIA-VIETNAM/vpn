package engine

import (
	"context"
	"encoding/binary"
	"runtime"
	"sync"

	"vpn/internal/bufpool"
	"vpn/internal/ipsec"
)

// Parallel receive. Decrypting is the one expensive step of the inbound path,
// and with 3DES (which legacy servers still choose over the AES this client
// offers first) it is expensive enough — about 25 times AES — to pin one core
// at a few tens of Mbit/s. parallelRx spreads it over several goroutines while
// delivering packets in exactly the order they arrived, so TCP inside the
// tunnel never sees reordering it would mistake for loss.
//
//	socket -> dispatch -+-> worker (verify + decrypt) -+
//	                    +-> worker                     +-> Recv, in arrival order
//	                    +-> worker                    -+
//
// It is used only for the data phase of a 3DES session (see
// espTransport.startParallel): AES is fast enough on one core that the extra
// hand-offs would cost more than they save.

// rxJob is one inbound datagram on its way through the workers.
type rxJob struct {
	pkt  []byte        // the ESP datagram, pooled
	msg  []byte        // the L2TP message it carried, if ok
	base []byte        // the pooled buffer msg points into
	ok   bool          // false: nothing to deliver (keepalive, replay, corrupt, not ours)
	done chan struct{} // capacity 1: signalled by the worker when the fields above are set
}

type parallelRx struct {
	order   chan *rxJob // every job, in arrival order
	work    chan *rxJob // the same jobs, for whichever worker is free
	jobs    sync.Pool
	stopped sync.WaitGroup
}

// rxQueueLen bounds packets inside the pipeline (decrypted or waiting).
const rxQueueLen = 256

// parallelRxWorkers is how many goroutines decrypt, or 0 when one core is all there is.
func parallelRxWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n < 2 {
		return 0
	}
	if n > 4 {
		n = 4
	}
	return n
}

// startParallel switches Recv to the parallel pipeline until ctx ends, when
// this session's cipher warrants it, and returns a function that stops the
// pipeline and switches Recv back; the caller must call it, after ctx is
// cancelled and its own Recv loop has returned, before anyone calls Recv again.
// It returns nil when the session stays on the plain path.
func (t *espTransport) startParallel(ctx context.Context) (stop func()) {
	workers := parallelRxWorkers()
	if workers == 0 || t.sas.current().in.Cipher != ipsec.Cipher3DESCBC {
		return nil
	}
	p := &parallelRx{
		order: make(chan *rxJob, rxQueueLen),
		work:  make(chan *rxJob, rxQueueLen),
	}
	p.jobs.New = func() any { return &rxJob{done: make(chan struct{}, 1)} }

	p.stopped.Add(1 + workers)
	go func() { // dispatch
		defer p.stopped.Done()
		defer close(p.work)
		for {
			pkt, err := t.mux.recv(ctx)
			if err != nil {
				return
			}
			j := p.jobs.Get().(*rxJob)
			j.pkt = pkt
			// order first: a job must be queued for delivery before a worker
			// can finish it.
			select {
			case p.order <- j:
			case <-ctx.Done():
				return
			}
			select {
			case p.work <- j:
			case <-ctx.Done():
				return
			}
		}
	}()
	for i := 0; i < workers; i++ {
		go func() {
			defer p.stopped.Done()
			// One Decryptor per SA this worker has seen; SAs change only at a
			// rekey, so the map stays at one or two entries.
			decs := map[*ipsec.SA]*ipsec.Decryptor{}
			decrypt := func(sa *ipsec.SA, pkt []byte) ([]byte, byte, error) {
				d := decs[sa]
				if d == nil {
					if len(decs) >= 4 {
						clear(decs) // SAs long since replaced
					}
					d = sa.NewDecryptor()
					decs[sa] = d
				}
				return d.Decrypt(pkt)
			}
			for j := range p.work {
				j.msg, j.base, j.ok = t.open(j.pkt, decrypt)
				j.pkt = nil
				j.done <- struct{}{}
			}
		}()
	}
	t.par = p
	return func() {
		p.stopped.Wait()
		t.par = nil
	}
}

// recv returns the next deliverable message in arrival order.
func (p *parallelRx) recv(ctx context.Context, t *espTransport) ([]byte, error) {
	for {
		var j *rxJob
		select {
		case j = <-p.order:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		select {
		case <-j.done:
		case <-ctx.Done():
			return nil, ctx.Err() // j is abandoned to the GC, with whatever its worker still writes to it
		}
		msg, base, ok := j.msg, j.base, j.ok
		j.msg, j.base, j.ok = nil, nil, false
		p.jobs.Put(j)
		if ok {
			t.lastPayload = base
			return msg, nil
		}
	}
}

// open decrypts one inbound ESP datagram with decrypt and returns the L2TP
// message it carries together with the pooled buffer holding it, or ok=false
// for anything not to be delivered (keepalives, unknown or expired SPIs,
// replays, corrupt packets). A packet that authenticates counts as proof the
// server is alive — the watchdog relies on exactly this. Safe for concurrent
// use as long as decrypt is.
func (t *espTransport) open(pkt []byte, decrypt func(*ipsec.SA, []byte) ([]byte, byte, error)) (msg, base []byte, ok bool) {
	// pkt came from the reader's pool and decrypt copies what it returns, so
	// the datagram is dead once this function is done with it.
	defer bufpool.Put(pkt)
	if len(pkt) < 4 {
		return nil, nil, false // e.g. the server's 1-byte NAT keepalive
	}
	in := t.sas.inbound(binary.BigEndian.Uint32(pkt[0:4]))
	if in == nil {
		t.noteDrop("ESP packet for an unknown SPI dropped (an SA already deleted/expired, or not ours)", nil)
		return nil, nil, false
	}
	payload, nextHeader, err := decrypt(in, pkt)
	if err != nil {
		// A stray/replayed/corrupt ESP packet is not fatal to the session —
		// count it and keep waiting rather than aborting the whole tunnel
		// over one bad datagram.
		t.noteDrop("ESP packet failed to decrypt — dropped", err)
		return nil, nil, false
	}
	// Authenticated: the server is alive, whatever this packet turns out to be.
	if t.live != nil {
		t.live.touch()
	}
	// Deliberately no per-packet log line: one write to the log (an SSD write,
	// a map allocation, a syscall) per tunnelled packet cost more than the
	// packet itself. Traffic is summarised by the periodic "tunnel alive"
	// counters instead — and decrypted bytes are never logged, since this
	// path carries the MS-CHAPv2 exchange and every user packet in cleartext.
	if nextHeader != protoUDP || len(payload) < 8 {
		return nil, nil, false
	}
	return payload[8:], payload, true // strip the inner UDP header, keep the L2TP message
}
