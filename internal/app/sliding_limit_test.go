package app

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlidingLimiterPerKey(t *testing.T) {
	l := newSlidingLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("k") {
			t.Fatalf("hit %d refused", i)
		}
	}
	if l.allow("k") {
		t.Fatal("4th hit allowed")
	}
	if !l.allow("other") {
		t.Fatal("a different key shares no budget")
	}
}

// A public endpoint keyed on caller-chosen ids must not let unique probes
// grow the map forever: at the cap, unknown keys fail closed until dead
// keys age out.
func TestSlidingLimiterBoundedKeys(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	l := newSlidingLimiter(10, time.Minute)
	l.now = func() time.Time { return time.Unix(0, clock.Load()) }
	for i := 0; i < slidingLimiterMaxKeys; i++ {
		if !l.allow(fmt.Sprintf("probe-%d", i)) {
			t.Fatalf("key %d refused before cap", i)
		}
	}
	if l.allow("probe-new") {
		t.Fatal("unknown key admitted past the cap — all keys still live")
	}
	// Every tracked key is a single live hit; a repeat on a full-but-dead
	// map reclaims the whole thing once the window passes.
	clock.Add(2 * int64(time.Minute))
	if !l.allow("probe-new") {
		t.Fatal("dead keys did not age out")
	}
}
