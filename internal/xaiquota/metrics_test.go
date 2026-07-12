package xaiquota

import (
	"testing"
	"time"
)

func TestParseFreeUsageTokens(t *testing.T) {
	body := `{"code":"subscription:free-usage-exhausted","error":"You've used all the included free usage for model grok-4.5-build-free for now. Usage resets over a rolling 24-hour window — tokens (actual/limit): 1091108/1000000."}`
	a, l, ok := ParseFreeUsageTokens(body)
	if !ok || a != 1091108 || l != 1000000 {
		t.Fatalf("got ok=%v actual=%d limit=%d", ok, a, l)
	}
}

func TestBuildMetricsView(t *testing.T) {
	st := UsageStats{
		DayKey:    "2026-07-11",
		UsedToday: 100,
		UsedTotal: 200,
		QuotaByAuth: map[string]*AccountQuotaSnapshot{
			"a1": {AuthIndex: "a1", Actual: 900000, Limit: 1000000},
		},
		DefaultLimitPerAcct: DefaultFreeLimit,
	}
	v := BuildMetricsView(3, 2, 1, st)
	if v.XAITotal != 3 || v.QuotaKnownAccounts != 1 {
		t.Fatalf("inventory bad: %+v", v)
	}
	// default: known-only pool (no unobserved * 1e6)
	if v.QuotaTotalEst != 1000000 || v.QuotaTotalKnownOnly != 1000000 {
		t.Fatalf("total known-only est=%d known=%d", v.QuotaTotalEst, v.QuotaTotalKnownOnly)
	}
	if v.UnobservedAccounts != 2 {
		t.Fatalf("unobserved=%d", v.UnobservedAccounts)
	}
	if v.RollingUsedKnown != 900000 || v.RollingLimitKnown != 1000000 {
		t.Fatalf("rolling bad: %+v", v)
	}
	if v.UsedTotalDisplay != 900000 {
		t.Fatalf("used display=%d want floor known actual", v.UsedTotalDisplay)
	}
	// with unobserved estimate
	v2 := BuildMetricsViewOpts(3, 2, 1, st, true)
	if v2.QuotaTotalEst != 3000000 {
		t.Fatalf("with est total=%d want 3000000", v2.QuotaTotalEst)
	}
}

func TestObserveFreeQuotaDelta(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir + "/st.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.ObserveFreeQuota("a1", 1000, 1000000, now); err != nil {
		t.Fatal(err)
	}
	st := s.GetUsageStats()
	if st.UsedToday != 0 {
		t.Fatalf("first observe used_today=%d want 0", st.UsedToday)
	}
	if err := s.ObserveFreeQuota("a1", 1500, 1000000, now); err != nil {
		t.Fatal(err)
	}
	st = s.GetUsageStats()
	if st.UsedToday != 500 || st.UsedTotal != 500 {
		t.Fatalf("delta today=%d total=%d", st.UsedToday, st.UsedTotal)
	}
}

func TestSyncAuthCounters(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir + "/st.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.SyncAuthCounters(10, 1, 8000, now); err != nil {
		t.Fatal(err)
	}
	st := s.GetUsageStats()
	if st.EstimatedToday != 0 || st.RequestsToday != 0 {
		t.Fatalf("baseline should not count history: %+v", st)
	}
	if err := s.SyncAuthCounters(12, 1, 8000, now); err != nil {
		t.Fatal(err)
	}
	st = s.GetUsageStats()
	if st.RequestsToday != 2 || st.EstimatedToday != 16000 {
		t.Fatalf("delta bad today req=%d est=%d", st.RequestsToday, st.EstimatedToday)
	}
}

func TestAddUsageEventPerAuthAndZeroStreak(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir + "/st.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = s.AddUsageEvent("a1", 50000, false, now)
	_ = s.AddUsageEvent("a1", 0, false, now)
	_ = s.AddUsageEvent("a1", 0, false, now)
	_ = s.AddUsageEvent("a1", 0, false, now)
	_ = s.AddUsageEvent("a1", 0, false, now)
	_ = s.AddUsageEvent("a1", 0, false, now)
	st := s.GetUsageStats()
	if st.UsedToday != 50000 {
		t.Fatalf("used=%d", st.UsedToday)
	}
	u := st.UsageByAuth["a1"]
	if u == nil || u.RequestsToday != 6 || u.ZeroTokenOK != 5 {
		t.Fatalf("per-auth bad: %+v", u)
	}
	v := BuildMetricsView(1, 1, 0, st)
	if !v.DetailMissingAlert || v.ZeroTokenStreak < 5 {
		t.Fatalf("alert expected: %+v", v)
	}
	_ = s.AddUsageEvent("a1", 100, false, now)
	st = s.GetUsageStats()
	v = BuildMetricsView(1, 1, 0, st)
	if v.DetailMissingAlert || v.ZeroTokenStreak != 0 {
		t.Fatalf("streak should clear: %+v", v)
	}
}
func TestBuildMetricsViewUnobservedDefault(t *testing.T) {
	st := UsageStats{DefaultLimitPerAcct: DefaultFreeLimit}
	v := BuildMetricsViewOpts(522, 500, 22, st, true)
	if v.QuotaTotalEst != 522*DefaultFreeLimit {
		t.Fatalf("full pool est=%d want %d", v.QuotaTotalEst, 522*DefaultFreeLimit)
	}
	if v.UnobservedAccounts != 522 {
		t.Fatalf("unobs=%d", v.UnobservedAccounts)
	}
}
