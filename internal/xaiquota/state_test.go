package xaiquota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreMigratesLegacyJSONToSQLite(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "guard-state.json")
	legacy := legacyState{
		Version: 1,
		Accounts: map[string]*AccountRecord{
			"a1": {AuthIndex: "a1", State: StateAutoDisabled, DisableSource: SourcePluginAuto, Owner: Owner, RecoverAtMS: time.Now().Add(time.Hour).UnixMilli()},
		},
		Usage:         &UsageStats{DayKey: DayKeyShanghai(time.Now()), UsedTotal: 123, UsageByAuth: map[string]*AccountUsageSnapshot{"a1": {AuthIndex: "a1", UsedTotal: 123}}},
		ActionHistory: []ActionEvent{{Action: "cooldown", AuthIndex: "a1"}},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if filepath.Ext(s.DBPath()) != ".sqlite" {
		t.Fatalf("db path=%s", s.DBPath())
	}
	if rec := s.Get("a1"); rec == nil || rec.Owner != Owner {
		t.Fatalf("migrated account=%+v", rec)
	}
	if got := s.GetUsageStats(); got.UsedTotal != 123 || got.UsageByAuth["a1"] == nil {
		t.Fatalf("migrated usage=%+v", got)
	}
	if len(s.ListActions(10)) != 1 {
		t.Fatal("action history not migrated")
	}
	if _, err := os.Stat(legacyPath + ".migrated.bak"); err != nil {
		t.Fatalf("legacy backup: %v", err)
	}
}

func TestStoreCreatesSQLiteParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data", "guard.sqlite")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sqlite file not created: %v", err)
	}
}

func TestStorePersistAndDue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.Upsert(AccountRecord{
		AuthIndex:     "a1",
		Provider:      "xai",
		DisableSource: SourcePluginAuto,
		State:         StateAutoDisabled,
		Owner:         Owner,
		RecoverAtMS:   now.Add(-time.Second).UnixMilli(),
		DisabledAtMS:  now.Add(-time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	// reload
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	due := s2.DueAutoDisabled(now)
	if len(due) != 1 || due[0].AuthIndex != "a1" {
		t.Fatalf("due=%v", due)
	}
}

func TestAppendActionKeepsRequestSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state-action.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	longBody := strings.Repeat("x", actionBodyMax+500)
	if err := s.AppendAction(ActionEvent{
		Action:    "skip_parse",
		Source:    "passive",
		AuthIndex: "a1",
		HTTPCode:  429,
		Signal:    "429",
		Reason:    "short-window signal/reset unparsed",
		Provider:  "xai",
		Body:      longBody,
		Headers: map[string]string{
			"Retry-After":  "60",
			"X-Request-Id": strings.Repeat("id", 200),
		},
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	items := s2.ListActions(10)
	if len(items) != 1 {
		t.Fatalf("len=%d", len(items))
	}
	ev := items[0]
	if ev.Action != "skip_parse" || ev.HTTPCode != 429 {
		t.Fatalf("ev=%+v", ev)
	}
	if !strings.HasSuffix(ev.Body, "...") || len(ev.Body) != actionBodyMax+3 {
		t.Fatalf("body len=%d want=%d+...", len(ev.Body), actionBodyMax)
	}
	if ev.Headers["Retry-After"] != "60" {
		t.Fatalf("headers=%v", ev.Headers)
	}
	if !strings.HasSuffix(ev.Headers["X-Request-Id"], "...") || len(ev.Headers["X-Request-Id"]) > actionHeaderMax+3 {
		t.Fatalf("header not truncated: %q len=%d", ev.Headers["X-Request-Id"], len(ev.Headers["X-Request-Id"]))
	}
}

func TestDueSkipsUserManualAndPreDisabled(t *testing.T) {
	s, err := NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = s.Upsert(AccountRecord{
		AuthIndex:     "manual",
		DisableSource: SourceUserManual,
		State:         StateUserManualDisabled,
		Owner:         Owner,
		RecoverAtMS:   now.Add(-time.Second).UnixMilli(),
	})
	_ = s.Upsert(AccountRecord{
		AuthIndex:     "pre",
		DisableSource: SourcePluginAuto,
		State:         StateAutoDisabled,
		Owner:         Owner,
		PreDisabled:   true,
		RecoverAtMS:   now.Add(-time.Second).UnixMilli(),
	})
	_ = s.Upsert(AccountRecord{
		AuthIndex:     "ok",
		DisableSource: SourcePluginAuto,
		State:         StateAutoDisabled,
		Owner:         Owner,
		RecoverAtMS:   now.Add(-time.Second).UnixMilli(),
	})
	due := s.DueAutoDisabled(now)
	if len(due) != 1 || due[0].AuthIndex != "ok" {
		t.Fatalf("due=%v", due)
	}
}
