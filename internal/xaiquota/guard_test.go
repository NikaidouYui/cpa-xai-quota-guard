package xaiquota

import (
	"sync"
	"testing"
	"time"
)

type memAuth struct {
	mu    sync.Mutex
	files map[string]AuthFile
}

func newMemAuth(files ...AuthFile) *memAuth {
	m := &memAuth{files: map[string]AuthFile{}}
	for _, f := range files {
		m.files[f.AuthIndex] = f
	}
	return m
}

func (m *memAuth) List() ([]AuthFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]AuthFile, 0, len(m.files))
	for _, f := range m.files {
		out = append(out, f)
	}
	return out, nil
}

func (m *memAuth) SetDisabled(authIndex string, disabled bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[authIndex]
	if !ok {
		return false, nil
	}
	prev := f.Disabled
	f.Disabled = disabled
	m.files[authIndex] = f
	return prev, nil
}

type memLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *memLog) Log(level, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, level+":"+message)
}

func TestGuardDisableAndRecoverPluginAuto(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x1", Name: "xai-1.json", Provider: "xai", Disabled: false})
	logger := &memLog{}
	g, err := NewGuard(Config{
		Enabled:         true,
		TickSeconds:     1,
		ManagementURL:   "http://127.0.0.1:8317",
		ManagementKey:   "test",
		StatePath:       "",
		MaxResetSeconds: 86400,
	}, auth, logger)
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x1",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached for requests per minute","type":"tokens","retry_after":1}}`,
	})
	// force recover_at to past
	rec := g.storeGet("x1")
	if rec == nil || rec.State != StateAutoDisabled || rec.DisableSource != SourcePluginAuto {
		t.Fatalf("expected auto_disabled, got %#v", rec)
	}
	rec.RecoverAtMS = time.Now().Add(-time.Second).UnixMilli()
	_ = g.storeUpsert(*rec)

	g.Tick()
	files, _ := auth.List()
	if files[0].Disabled {
		t.Fatal("expected re-enabled")
	}
	after := g.storeGet("x1")
	if after != nil && after.State != StateActive {
		// MarkActive keeps record but state active; Get still returns it
		if after.DisableSource != SourceNone {
			t.Fatalf("after recover %#v", after)
		}
	}
}

func TestGuardNeverRecoversUserManual(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x2", Name: "xai-2.json", Provider: "xai", Disabled: true})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x2",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"code":"rate_limit_exceeded","message":"Rate limit","type":"tokens","retry_after":1}}`,
	})
	rec := g.storeGet("x2")
	if rec == nil || rec.DisableSource != SourceUserManual {
		t.Fatalf("expected user_manual, got %#v", rec)
	}
	// Force due
	rec.RecoverAtMS = time.Now().Add(-time.Second).UnixMilli()
	rec.State = StateUserManualDisabled
	_ = g.storeUpsert(*rec)
	g.Tick()
	files, _ := auth.List()
	if !files[0].Disabled {
		t.Fatal("user_manual must remain disabled")
	}
}

func TestGuardSkipsParseFailure(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x3", Name: "xai-3.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x3",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"code":"rate_limit_exceeded","message":"Rate limit"}}`, // no reset time
	})
	files, _ := auth.List()
	if files[0].Disabled {
		t.Fatal("must not disable when reset parse fails")
	}
}