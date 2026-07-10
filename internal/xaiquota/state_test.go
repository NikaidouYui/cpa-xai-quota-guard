package xaiquota

import (
	"path/filepath"
	"testing"
	"time"
)

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