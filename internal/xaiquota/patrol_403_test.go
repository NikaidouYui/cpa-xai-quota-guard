package xaiquota

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func runEnabledProbe(t *testing.T, status int, body string) (*Guard, *memAuth, probeResult) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	name := "xai-enabled.json"
	raw := []byte(fmt.Sprintf(`{"access_token":"token","base_url":%q,"disabled":false}`, srv.URL))
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	auth := newMemAuth(AuthFile{AuthIndex: "xe", Name: name, Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{Enabled: true, ManagementURL: "http://cpa", ManagementKey: "key", PatrolModel: "grok-test", MaxResetSeconds: 86400}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	r := g.probeOneCredential(AuthFile{AuthIndex: "xe", Name: name, Provider: "xai", Disabled: false}, dir, srv.Client(), "all")
	return g, auth, r
}

// TestPatrolDisablesPermissionDenied403: 真 403 应禁用而非删除。
func TestPatrolDisablesPermissionDenied403(t *testing.T) {
	g, auth, r := runEnabledProbe(t, http.StatusForbidden,
		`{"code":"permission-denied","error":"Access to the chat endpoint is denied. Please ensure you're using the correct credentials."}`)
	if r.action != "disabled" {
		t.Fatalf("result=%+v want action=disabled", r)
	}
	files, _ := auth.List()
	if len(files) != 1 {
		t.Fatalf("must keep credential file: %+v", files)
	}
	if !files[0].Disabled {
		t.Fatal("must soft-disable")
	}
	rec := g.storeGet("xe")
	if rec == nil || rec.Signal != "permission_denied" || rec.RecoverAtMS != 0 {
		t.Fatalf("tracked=%+v", rec)
	}
}

// TestPatrolStillDeletesInvalidCredentials401: 401 仍删除（与 403 路径分离）。
func TestPatrolStillDeletesInvalidCredentials401(t *testing.T) {
	_, auth, r := runEnabledProbe(t, http.StatusUnauthorized,
		`{"error":"Invalid or expired credentials (auth_kind=bearer, x_xai_token_auth=xai-grok-cli, upstream=PermissionDenied, reason=no auth context)"}`)
	if r.action != "deleted" {
		t.Fatalf("result=%+v want action=deleted", r)
	}
	files, _ := auth.List()
	if len(files) != 0 {
		t.Fatalf("401 must delete credential, still have %+v", files)
	}
}

// TestPatrolPermissionDenied200Reenables: 复核 200 时恢复 permission_denied 禁用号。
func TestPatrolPermissionDenied200Reenables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	name := "xai-perm.json"
	raw := []byte(fmt.Sprintf(`{"access_token":"token","base_url":%q,"disabled":true}`, srv.URL))
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	auth := newMemAuth(AuthFile{AuthIndex: "xp", Name: name, Provider: "xai", Disabled: true})
	g, err := NewGuard(Config{Enabled: true, ManagementURL: "http://cpa", ManagementKey: "key", PatrolModel: "grok-test", MaxResetSeconds: 86400}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	_ = g.storeUpsert(AccountRecord{
		AuthIndex: "xp", FileName: name, Provider: "xai",
		DisableSource: SourcePluginAuto, State: StateAutoDisabled,
		RecoverAtMS: 0, Owner: Owner, Signal: "permission_denied", Reason: "prior 403",
	})
	r := g.probeOneCredential(AuthFile{AuthIndex: "xp", Name: name, Provider: "xai", Disabled: true}, dir, srv.Client(), "spending_only")
	if r.action != "reenabled" {
		t.Fatalf("result=%+v want reenabled", r)
	}
	files, _ := auth.List()
	if len(files) != 1 || files[0].Disabled {
		t.Fatalf("must re-enable: %+v", files)
	}
	if rec := g.storeGet("xp"); rec != nil && rec.State == StateAutoDisabled {
		t.Fatalf("store must clear auto_disabled: %+v", rec)
	}
}
