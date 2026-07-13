package xaiquota

import "testing"

func TestSelectPatrolLogInterestingFirst(t *testing.T) {
	src := make([]patrolLogEntry, 0, 100)
	// oldest first in memory (as append order)
	for i := 0; i < 90; i++ {
		src = append(src, patrolLogEntry{Action: "alive", HTTPCode: 200, AuthIndex: "a", TimeMS: int64(i)})
	}
	src = append(src, patrolLogEntry{Action: "cooldown", HTTPCode: 429, AuthIndex: "c1", TimeMS: 100})
	src = append(src, patrolLogEntry{Action: "deleted", HTTPCode: 403, AuthIndex: "d1", TimeMS: 101})
	// more alive after
	src = append(src, patrolLogEntry{Action: "alive", HTTPCode: 200, AuthIndex: "z", TimeMS: 102})

	out := selectPatrolLogForUI(src, 10)
	if len(out) != 10 {
		t.Fatalf("len=%d", len(out))
	}
	// newest first among selected: deleted/cooldown should appear before pure alive flood
	var sawNonAlive bool
	for _, e := range out {
		if e.Action != "alive" {
			sawNonAlive = true
			break
		}
	}
	if !sawNonAlive {
		t.Fatalf("expected non-alive in first page, got all alive")
	}
	// full small log
	small := []patrolLogEntry{{Action: "alive", TimeMS: 1}, {Action: "cooldown", TimeMS: 2}}
	out2 := selectPatrolLogForUI(small, 50)
	if len(out2) != 2 || out2[0].Action != "cooldown" {
		t.Fatalf("small newest-first: %+v", out2)
	}
}