package xaiquota

import "testing"

func TestResolvePatrolWorkersCapsByUserMax(t *testing.T) {
	w := resolvePatrolWorkers(2, 100)
	if w > 2 || w < 1 {
		t.Fatalf("workers=%d want in [1,2]", w)
	}
	w = resolvePatrolWorkers(0, 3)
	if w < 1 || w > 3 {
		t.Fatalf("default workers=%d invalid for 3 candidates", w)
	}
	w = resolvePatrolWorkers(32, 1)
	if w != 1 {
		t.Fatalf("workers=%d want 1 when only 1 candidate", w)
	}
}
