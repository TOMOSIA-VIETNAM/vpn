package bufpool

import "testing"

func TestGetPut(t *testing.T) {
	b := Get(100)
	if len(b) != 100 || cap(b) != Size {
		t.Fatalf("Get(100): len=%d cap=%d", len(b), cap(b))
	}
	Put(b)
	big := Get(Size + 1)
	if len(big) != Size+1 {
		t.Fatalf("oversize Get: len=%d", len(big))
	}
	Put(big)                  // not pooled, must not panic
	Put(make([]byte, 10, 20)) // foreign slice, must not panic
}

func BenchmarkGetPut(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Put(Get(1500))
	}
}
