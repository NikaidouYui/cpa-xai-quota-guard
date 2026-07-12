package xaiquota

import (
	"testing"
	"time"
)

func TestDayRangeShanghai(t *testing.T) {
	from, to := DayRangeShanghai(mustParse("2026-07-11T04:00:00Z")) // 12:00 CST
	if to-from != 24*3600*1000 {
		t.Fatalf("span %d", to-from)
	}
	if DayKeyShanghai(mustParse("2026-07-11T04:00:00Z")) != "2026-07-11" {
		t.Fatal("day key")
	}
}

func mustParse(s string) time.Time {
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tt
}