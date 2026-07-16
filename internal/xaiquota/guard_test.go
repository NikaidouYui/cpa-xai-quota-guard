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

func (m *memAuth) Delete(authIndex string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, authIndex)
	return nil
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

func TestTickReconcilesProvisionalCooldownWhenCPAStayedEnabled(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "xp", Name: "xai-pending.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{Enabled: true, ManagementURL: "http://127.0.0.1:8317", ManagementKey: "test"}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.storeUpsert(AccountRecord{
		AuthIndex: "xp", FileName: "xai-pending.json", Provider: "xai",
		State: StateAutoDisabled, DisableSource: SourcePluginAuto, Owner: Owner,
		RecoverAtMS: time.Now().Add(time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	g.Tick()
	rec := g.storeGet("xp")
	if rec == nil || rec.State != StateActive || rec.DisableSource != SourceNone {
		t.Fatalf("reconciled record=%+v", rec)
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

func TestGuardCooldownModelCapacity(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "xcap", Name: "xai-cap.json", Provider: "xai", Disabled: false, Account: "cap@test"})
	g, err := NewGuard(Config{
		Enabled:         true,
		ManagementURL:   "http://127.0.0.1:8317",
		ManagementKey:   "test",
		MaxResetSeconds: 86400,
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"sequence_number":0,"type":"error","code":null,"message":"The model is currently at capacity due to high demand. Please try again in a few minutes, or use a higher service tier for priority processing: https://docs.x.ai/developers/advanced-api-usage/priority-processing","param":null}`
	g.HandleUsage(UsageEvent{
		AuthIndex:  "xcap",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       body,
	})
	files, _ := auth.List()
	if len(files) != 1 || !files[0].Disabled {
		t.Fatalf("expected soft-disable for capacity, files=%#v", files)
	}
	rec := g.storeGet("xcap")
	if rec == nil || rec.Signal != "model_capacity" {
		t.Fatalf("record=%#v", rec)
	}
	wait := time.Until(time.UnixMilli(rec.RecoverAtMS))
	if wait < 4*time.Minute || wait > 6*time.Minute {
		t.Fatalf("capacity wait=%v want ~5m", wait)
	}
	// Longer free-usage wait must not be shortened by a later capacity event.
	_ = g.storeUpsert(AccountRecord{
		AuthIndex: "xcap", FileName: "xai-cap.json", Provider: "xai", Account: "cap@test",
		DisableSource: SourcePluginAuto, State: StateAutoDisabled, Owner: Owner,
		RecoverAtMS: time.Now().Add(12 * time.Hour).UnixMilli(),
		Signal:      "body.error.code=subscription:free-usage-exhausted",
		Reason:      "免费额度用尽",
	})
	g.HandleUsage(UsageEvent{
		AuthIndex: "xcap", Provider: "xai", Failed: true, StatusCode: 429, Body: body,
	})
	rec2 := g.storeGet("xcap")
	if rec2 == nil {
		t.Fatal("missing record after extend")
	}
	if time.Until(time.UnixMilli(rec2.RecoverAtMS)) < 10*time.Hour {
		t.Fatalf("capacity must not shorten free-usage cooldown: recover in %v", time.Until(time.UnixMilli(rec2.RecoverAtMS)))
	}
	if rec2.Signal == "model_capacity" {
		t.Fatalf("must keep free-usage signal, got %q", rec2.Signal)
	}
}

func TestGuardDisablesPermissionDenied403(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x403", Name: "xai-403.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x403",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 403,
		Body:       `{"code":"permission-denied","error":"Access to the chat endpoint is denied."}`,
	})
	files, _ := auth.List()
	if len(files) != 1 {
		t.Fatalf("403 must NOT delete credential, got %#v", files)
	}
	if !files[0].Disabled {
		t.Fatal("expected soft-disabled for 403 permission-denied")
	}
	rec := g.storeGet("x403")
	if rec == nil || rec.State != StateAutoDisabled || rec.DisableSource != SourcePluginAuto {
		t.Fatalf("expected auto_disabled plugin_auto, got %#v", rec)
	}
	if rec.Signal != "permission_denied" {
		t.Fatalf("signal=%q want permission_denied", rec.Signal)
	}
	if rec.RecoverAtMS != 0 {
		t.Fatalf("permission_denied must not auto-recover: recover_at_ms=%d", rec.RecoverAtMS)
	}
	// Tick must not re-enable permanent 403 disable.
	g.Tick()
	files, _ = auth.List()
	if !files[0].Disabled {
		t.Fatal("Tick must not re-enable permission_denied")
	}
}

func TestGuardDoesNotDeleteContentSafety(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "xcsam", Name: "xai-csam.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	// Same shape as production mis-delete: permission-denied + CSAM safety check, http_code 0.
	g.HandleUsage(UsageEvent{
		AuthIndex:  "xcsam",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 0,
		Body:       `{"code":"permission-denied","error":"Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE_CSAM"}`,
	})
	files, _ := auth.List()
	if len(files) != 1 {
		t.Fatalf("content safety must NOT delete credential, got %#v", files)
	}
	if files[0].Disabled {
		t.Fatal("content safety must not disable either")
	}
}
func TestGuardRecordsSuccessUsageTokens(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "xs", Name: "xai-s.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
		StatePath:     "",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:    "xs",
		Provider:     "xai",
		Failed:       false,
		StatusCode:   200,
		InputTokens:  50000,
		OutputTokens: 800,
		TotalTokens:  50800,
	})
	st := g.store.GetUsageStats()
	if st.UsedToday != 50800 || st.RequestsToday != 1 || st.SuccessEvents != 1 {
		t.Fatalf("success metrics not recorded: %+v", st)
	}
	// non-xai ignored
	g.HandleUsage(UsageEvent{Provider: "codex", Failed: false, TotalTokens: 9999})
	st = g.store.GetUsageStats()
	if st.UsedToday != 50800 {
		t.Fatalf("non-xai leaked into today: %+v", st)
	}
	// disabled plugin ignores (mutate config in-place to keep same store)
	g.mu.Lock()
	g.cfg.Enabled = false
	g.mu.Unlock()
	g.HandleUsage(UsageEvent{Provider: "xai", Failed: false, TotalTokens: 1000})
	st = g.store.GetUsageStats()
	if st.UsedToday != 50800 {
		t.Fatalf("disabled still counted: %+v", st)
	}
}
func TestGuardDeletesInvalidCredentials401(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x401", Name: "xai-401.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x401",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 401,
		Body:       `{"error":"Invalid or expired credentials (auth_kind=bearer, x_xai_token_auth=xai-grok-cli, upstream=PermissionDenied, reason=no auth context)"}`,
	})
	files, _ := auth.List()
	if len(files) != 0 {
		t.Fatalf("expected deleted, still have %#v", files)
	}
}

func TestGuardCooldownsSpendingLimit402(t *testing.T) {
	auth := newMemAuth(AuthFile{AuthIndex: "x402", Name: "xai-402.json", Provider: "xai", Disabled: false})
	g, err := NewGuard(Config{
		Enabled:       true,
		ManagementURL: "http://127.0.0.1:8317",
		ManagementKey: "test",
	}, auth, &memLog{})
	if err != nil {
		t.Fatal(err)
	}
	g.HandleUsage(UsageEvent{
		AuthIndex:  "x402",
		Provider:   "xai",
		Failed:     true,
		StatusCode: 402,
		Body:       `{"code":"personal-team-blocked:spending-limit","error":"You have run out of credits or need a Grok subscription."}`,
	})
	files, _ := auth.List()
	if len(files) != 1 {
		t.Fatalf("expected keep file (cooldown), got %#v", files)
	}
	if !files[0].Disabled {
		t.Fatal("expected plugin_auto disabled for 402 spending-limit")
	}
	rec := g.storeGet("x402")
	if rec == nil || rec.State != StateAutoDisabled || rec.DisableSource != SourcePluginAuto {
		t.Fatalf("expected auto_disabled plugin_auto, got %#v", rec)
	}
	if rec.Signal != "spending_limit" {
		t.Fatalf("signal=%q want spending_limit", rec.Signal)
	}
}
