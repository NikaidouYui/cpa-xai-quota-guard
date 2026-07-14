package xaiquota

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func runCPADisabledProbe(t *testing.T, status int, body string) (*Guard, *memAuth, probeResult) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	name := "xai-disabled.json"
	raw := []byte(fmt.Sprintf(`{"access_token":"token","base_url":%q,"disabled":true}`, srv.URL))
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	auth := newMemAuth(AuthFile{AuthIndex: "xd", Name: name, Provider: "xai", Disabled: true})
	g, err := NewGuard(Config{Enabled: true, ManagementURL: "http://cpa", ManagementKey: "key", PatrolModel: "grok-test", MaxResetSeconds: 86400}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	r := g.probeOneCredential(AuthFile{AuthIndex: "xd", Name: name, Provider: "xai", Disabled: true}, dir, srv.Client(), "cpa_disabled")
	return g, auth, r
}

func TestCPADisabledProbe200Reenables(t *testing.T) {
	_, auth, r := runCPADisabledProbe(t, http.StatusOK, `{"ok":true}`)
	if r.action != "reenabled_external" {
		t.Fatalf("result=%+v", r)
	}
	files, _ := auth.List()
	if len(files) != 1 || files[0].Disabled {
		t.Fatalf("files=%+v", files)
	}
}

func TestCPADisabledProbe429TracksAndKeepsDisabled(t *testing.T) {
	body := `{"code":"subscription:free-usage-exhausted","error":"You've used all included free usage. Usage resets over a rolling 24-hour window - tokens (actual/limit): 100/100."}`
	g, auth, r := runCPADisabledProbe(t, http.StatusTooManyRequests, body)
	if r.action != "cooldown" {
		t.Fatalf("result=%+v", r)
	}
	rec := g.storeGet("xd")
	if rec == nil || rec.State != StateAutoDisabled || rec.Owner != Owner || rec.PreDisabled {
		t.Fatalf("tracked=%+v", rec)
	}
	files, _ := auth.List()
	if len(files) != 1 || !files[0].Disabled {
		t.Fatalf("files=%+v", files)
	}
}

func TestCPADisabledProbe403ExcludedWithoutDelete(t *testing.T) {
	g, auth, r := runCPADisabledProbe(t, http.StatusForbidden, `{"code":"permission-denied","error":"Access to the chat endpoint is denied. Please ensure you're using the correct credentials."}`)
	if r.action != "external_invalid" {
		t.Fatalf("result=%+v", r)
	}
	if rec := g.storeGet("xd"); rec != nil {
		t.Fatalf("must not track invalid external credential: %+v", rec)
	}
	files, _ := auth.List()
	if len(files) != 1 || !files[0].Disabled {
		t.Fatalf("must remain disabled and not deleted: %+v", files)
	}
}
