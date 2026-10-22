//go:build unix

package fill

import "testing"

func TestBridgeLockExclusive(t *testing.T) {
	dir := t.TempDir()
	first, err := BridgeLock(dir)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	second, err := BridgeLock(dir)
	if err != nil {
		t.Fatalf("second lock: %v", err)
	}
	if second != nil {
		t.Fatal("second bridge acquired the lock — duplicates must refuse")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}
	third, err := BridgeLock(dir)
	if err != nil {
		t.Fatalf("relock after release: %v", err)
	}
	if third == nil {
		t.Fatal("lock not reacquired after release")
	}
	_ = third.Close()
}
