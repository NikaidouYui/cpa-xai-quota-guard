package xaiquota

import (
	"testing"
)

func TestSelectIncrementalBatchWrap(t *testing.T) {
	mk := func(ids ...string) []AuthFile {
		out := make([]AuthFile, 0, len(ids))
		for _, id := range ids {
			out = append(out, AuthFile{AuthIndex: id, Name: id + ".json", Provider: "xai"})
		}
		return out
	}
	// Unsorted input → sorted by auth_index
	all := mk("c", "a", "b", "d", "e")
	batch, next := selectIncrementalBatch(all, "", 2)
	if len(batch) != 2 || batch[0].AuthIndex != "a" || batch[1].AuthIndex != "b" {
		t.Fatalf("first batch=%+v", batch)
	}
	if next != "c" {
		t.Fatalf("next=%q want c", next)
	}
	batch2, next2 := selectIncrementalBatch(all, next, 2)
	if len(batch2) != 2 || batch2[0].AuthIndex != "c" || batch2[1].AuthIndex != "d" {
		t.Fatalf("second batch=%+v", batch2)
	}
	if next2 != "e" {
		t.Fatalf("next2=%q want e", next2)
	}
	// wrap past end
	batch3, next3 := selectIncrementalBatch(all, next2, 2)
	if len(batch3) != 2 || batch3[0].AuthIndex != "e" || batch3[1].AuthIndex != "a" {
		t.Fatalf("wrap batch=%+v", batch3)
	}
	if next3 != "b" {
		t.Fatalf("next3=%q want b", next3)
	}
}

func TestSelectIncrementalBatchMissingCursor(t *testing.T) {
	all := []AuthFile{
		{AuthIndex: "a1", Name: "a.json"},
		{AuthIndex: "b1", Name: "b.json"},
		{AuthIndex: "c1", Name: "c.json"},
	}
	// Cursor between a1 and b1 points at b1
	batch, next := selectIncrementalBatch(all, "a5", 1)
	if len(batch) != 1 || batch[0].AuthIndex != "b1" {
		t.Fatalf("batch=%+v", batch)
	}
	if next != "c1" {
		t.Fatalf("next=%q", next)
	}
	// Cursor after all keys → wrap to start
	batch2, next2 := selectIncrementalBatch(all, "z9", 2)
	if len(batch2) != 2 || batch2[0].AuthIndex != "a1" || batch2[1].AuthIndex != "b1" {
		t.Fatalf("wrap-from-tail batch=%+v", batch2)
	}
	if next2 != "c1" {
		t.Fatalf("next2=%q", next2)
	}
}

func TestSelectIncrementalBatchFullPool(t *testing.T) {
	all := []AuthFile{{AuthIndex: "a"}, {AuthIndex: "b"}}
	batch, next := selectIncrementalBatch(all, "a", 10)
	if len(batch) != 2 || next != "" {
		t.Fatalf("full pass batch=%+v next=%q", batch, next)
	}
}

func TestStoreIncrementalCursor(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir + "/state.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if s.GetIncrementalCursor() != "" {
		t.Fatal("empty default")
	}
	if err := s.SetIncrementalCursor("auth-42"); err != nil {
		t.Fatal(err)
	}
	if got := s.GetIncrementalCursor(); got != "auth-42" {
		t.Fatalf("got %q", got)
	}
	// reopen
	s2, err := NewStore(dir + "/state.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	if got := s2.GetIncrementalCursor(); got != "auth-42" {
		t.Fatalf("persist got %q", got)
	}
}
