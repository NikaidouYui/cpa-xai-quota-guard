package xaiquota

import (
	"strings"
	"testing"
)

func TestParseIdentityFromFileName(t *testing.T) {
	cases := []struct {
		name string
		want string
		ok   bool
	}{
		{"xai_oauth_xfwp7_mygo.qzz.io_20260712T063216Z.json", "xfwp7_mygo.qzz.io", true},
		{"xai_oauth_xfwp7_mygo.qzz.io_20260712T134620Z.json", "xfwp7_mygo.qzz.io", true},
		{"xai-user@example.com.json", "user@example.com", true},
		{"xai_oauth_user@mail.com_20260101T000000Z.json", "user@mail.com", true},
		{"random.json", "", false},
		{"xai-ab.json", "", false}, // too short / no domain markers
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseIdentityFromFileName(tc.name)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ParseIdentityFromFileName(%q)=(%q,%v) want (%q,%v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFileNameRecencyMS(t *testing.T) {
	a := FileNameRecencyMS("xai_oauth_xfwp7_mygo.qzz.io_20260712T063216Z.json")
	b := FileNameRecencyMS("xai_oauth_xfwp7_mygo.qzz.io_20260712T134620Z.json")
	if a <= 0 || b <= 0 {
		t.Fatalf("expected positive recency a=%d b=%d", a, b)
	}
	if b <= a {
		t.Fatalf("later stamp must rank higher: a=%d b=%d", a, b)
	}
	if FileNameRecencyMS("xai-user@x.com.json") != 0 {
		t.Fatal("no stamp should return 0")
	}
}

func TestPlanDedupeXAI_KeepNewest(t *testing.T) {
	files := []AuthFile{
		{AuthIndex: "old", Name: "xai_oauth_xfwp7_mygo.qzz.io_20260712T063216Z.json", Provider: "xai", Account: "xfwp7_mygo.qzz.io", Disabled: true},
		{AuthIndex: "new", Name: "xai_oauth_xfwp7_mygo.qzz.io_20260712T134620Z.json", Provider: "xai", Account: "xfwp7_mygo.qzz.io", Disabled: true},
		{AuthIndex: "solo", Name: "xai_oauth_onlyone@x.com_20260712T100000Z.json", Provider: "xai", Account: "onlyone@x.com"},
		{AuthIndex: "claude", Name: "claude-a.json", Provider: "claude", Account: "a@b.com"},
		// same identity via filename only (no account field)
		{AuthIndex: "n1", Name: "xai_oauth_dup@mail.com_20260101T010000Z.json", Provider: "xai"},
		{AuthIndex: "n2", Name: "xai_oauth_dup@mail.com_20260301T010000Z.json", Provider: "xai"},
	}
	plan := PlanDedupeXAI(files)
	if plan.ScannedXAI != 5 {
		t.Fatalf("scanned_xai=%d want 5", plan.ScannedXAI)
	}
	if plan.GroupCount != 2 {
		t.Fatalf("group_count=%d want 2 (solo excluded)", plan.GroupCount)
	}
	if plan.DeleteCount != 2 || plan.KeepCount != 2 {
		t.Fatalf("keep=%d delete=%d want 2/2", plan.KeepCount, plan.DeleteCount)
	}

	var foundMygo bool
	for _, g := range plan.Groups {
		if strings.Contains(g.Identity, "mygo") {
			foundMygo = true
			if g.Keep.AuthIndex != "new" {
				t.Fatalf("mygo keep=%s want new", g.Keep.AuthIndex)
			}
			if len(g.Delete) != 1 || g.Delete[0].AuthIndex != "old" {
				t.Fatalf("mygo delete=%+v want [old]", g.Delete)
			}
		}
		if strings.Contains(g.Identity, "dup@mail.com") {
			if g.Keep.AuthIndex != "n2" {
				t.Fatalf("dup keep=%s want n2", g.Keep.AuthIndex)
			}
		}
	}
	if !foundMygo {
		t.Fatal("missing mygo group")
	}
}

func TestPlanDedupeXAI_ModTimeFallback(t *testing.T) {
	files := []AuthFile{
		{AuthIndex: "a", Name: "xai-same@x.com.json", Provider: "xai", Account: "same@x.com", ModTimeMS: 1000},
		{AuthIndex: "b", Name: "xai-same@x.com-2.json", Provider: "xai", Account: "same@x.com", ModTimeMS: 2000},
	}
	// Note: second file identity from account field; names differ but same account.
	plan := PlanDedupeXAI(files)
	if plan.DeleteCount != 1 {
		t.Fatalf("delete=%d want 1", plan.DeleteCount)
	}
	if plan.Groups[0].Keep.AuthIndex != "b" {
		t.Fatalf("keep=%s want b (higher modtime)", plan.Groups[0].Keep.AuthIndex)
	}
}

func TestDedupeIdentity_PrefersAccount(t *testing.T) {
	f := AuthFile{
		Account:  "Real@Mail.Com",
		Name:     "xai_oauth_other@x.com_20260712T134620Z.json",
		Provider: "xai",
	}
	if got := DedupeIdentity(f); got != "real@mail.com" {
		t.Fatalf("got %q", got)
	}
}

func TestExecuteDedupeXAI_DeletesOlder(t *testing.T) {
	auth := newMemAuth(
		AuthFile{AuthIndex: "old", Name: "xai_oauth_xfwp7_mygo.qzz.io_20260712T063216Z.json", Provider: "xai", Account: "xfwp7_mygo.qzz.io"},
		AuthFile{AuthIndex: "new", Name: "xai_oauth_xfwp7_mygo.qzz.io_20260712T134620Z.json", Provider: "xai", Account: "xfwp7_mygo.qzz.io"},
		AuthFile{AuthIndex: "solo", Name: "xai_oauth_solo@x.com_20260712T100000Z.json", Provider: "xai", Account: "solo@x.com"},
	)
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := g.ExecuteDedupeXAI(true)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Plan.DeleteCount != 1 || preview.DeletedCount != 0 {
		t.Fatalf("dry-run plan delete=%d deleted=%d", preview.Plan.DeleteCount, preview.DeletedCount)
	}
	res, err := g.ExecuteDedupeXAI(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedCount != 1 || res.FailedCount != 0 {
		t.Fatalf("deleted=%d failed=%d", res.DeletedCount, res.FailedCount)
	}
	files, _ := auth.List()
	if len(files) != 2 {
		t.Fatalf("remaining files=%d want 2", len(files))
	}
	for _, f := range files {
		if f.AuthIndex == "old" {
			t.Fatal("old duplicate should be deleted")
		}
	}
}
