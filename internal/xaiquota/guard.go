package xaiquota

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Config controls runtime behaviour of the guard.
type Config struct {
	Enabled         bool
	TickSeconds     float64
	ManagementURL   string
	ManagementKey   string
	StatePath       string
	MaxResetSeconds float64
}

// Defaults returns safe defaults. enabled=false until configured.
func Defaults() Config {
	return Config{
		Enabled:         false,
		TickSeconds:     15,
		StatePath:       "data/cpa-xai-quota-guard-state.json",
		MaxResetSeconds: 86400,
	}
}

// AuthFileLookup returns current auth file metadata from management API.
type AuthFileLookup interface {
	List() ([]AuthFile, error)
	SetDisabled(authIndex string, disabled bool) (prevDisabled bool, err error)
}

// AuthFile is the management API subset we need.
type AuthFile struct {
	AuthIndex string
	Name      string
	Provider  string
	Account   string
	Disabled  bool
}

// Logger writes plugin logs.
type Logger interface {
	Log(level, message string)
}

// UsageEvent is the plugin-side view of a usage failure.
type UsageEvent struct {
	AuthIndex       string
	Provider        string
	AuthType        string
	Account         string
	Failed          bool
	StatusCode      int
	Body            string
	ResponseHeaders map[string][]string
	EventHash       string
}

// Guard owns the disable/recover state machine.
type Guard struct {
	mu     sync.Mutex
	cfg    Config
	store  *Store
	auth   AuthFileLookup
	logger Logger

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewGuard constructs a guard with durable state.
func NewGuard(cfg Config, auth AuthFileLookup, logger Logger) (*Guard, error) {
	store, err := NewStore(cfg.StatePath)
	if err != nil {
		return nil, err
	}
	g := &Guard{
		cfg:    cfg,
		store:  store,
		auth:   auth,
		logger: logger,
	}
	return g, nil
}

func (g *Guard) ApplyConfig(cfg Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if cfg.TickSeconds <= 0 {
		cfg.TickSeconds = 15
	}
	if cfg.MaxResetSeconds <= 0 {
		cfg.MaxResetSeconds = 86400
	}
	if cfg.StatePath == "" {
		cfg.StatePath = "data/cpa-xai-quota-guard-state.json"
	}
	// Reload store if path changed.
	if g.store == nil || g.store.Path() != cfg.StatePath {
		store, err := NewStore(cfg.StatePath)
		if err != nil {
			g.logf("error", "reload state failed: %v", err)
		} else {
			g.store = store
		}
	}
	g.cfg = cfg
}

func (g *Guard) Config() Config {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cfg
}

func (g *Guard) Snapshot() map[string]AccountRecord {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		return map[string]AccountRecord{}
	}
	return store.Snapshot()
}

// StartTicker starts background recovery scans.
func (g *Guard) StartTicker() {
	g.mu.Lock()
	if g.stopCh != nil {
		g.mu.Unlock()
		return
	}
	g.stopCh = make(chan struct{})
	g.mu.Unlock()
	g.wg.Add(1)
	go g.tickerLoop()
}

// StopTicker stops background recovery.
func (g *Guard) StopTicker() {
	g.mu.Lock()
	if g.stopCh == nil {
		g.mu.Unlock()
		return
	}
	close(g.stopCh)
	g.stopCh = nil
	g.mu.Unlock()
	g.wg.Wait()
}

func (g *Guard) tickerLoop() {
	defer g.wg.Done()
	for {
		cfg := g.Config()
		interval := time.Duration(cfg.TickSeconds * float64(time.Second))
		if interval <= 0 {
			interval = 15 * time.Second
		}
		timer := time.NewTimer(interval)
		g.Tick()
		g.mu.Lock()
		stop := g.stopCh
		g.mu.Unlock()
		select {
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// HandleUsage processes one usage event.
func (g *Guard) HandleUsage(ev UsageEvent) {
	cfg := g.Config()
	if !cfg.Enabled {
		return
	}
	if !ev.Failed {
		return
	}
	authIndex := trim(ev.AuthIndex)
	if authIndex == "" {
		return
	}
	if !IsXAIProvider(ev.Provider, ev.AuthType) {
		return
	}

	headers := headerFromMap(ev.ResponseHeaders)
	match, ok := MatchShortWindowQuota(MatchInput{
		Provider:        ev.Provider,
		AuthType:        ev.AuthType,
		Failed:          true,
		StatusCode:      ev.StatusCode,
		Body:            ev.Body,
		ResponseHeaders: headers,
		Now:             time.Now(),
		MaxResetSeconds: cfg.MaxResetSeconds,
	})
	if !ok {
		// Time parse failure or non-short-window: silent skip with log.
		if ev.StatusCode == 429 {
			g.logf("info", "xAI 429 未满足短时额度条件(信号/重置时间)，跳过 auth=%s", authIndex)
		}
		return
	}

	g.disableForMatch(authIndex, ev, match)
}

func (g *Guard) disableForMatch(authIndex string, ev UsageEvent, match MatchResult) {
	cfg := g.Config()
	if cfg.ManagementURL == "" || cfg.ManagementKey == "" {
		g.logf("warn", "management 未配置，仅记录不禁用 auth=%s recover_at=%s", authIndex, match.RecoverAt.Format(time.RFC3339))
		return
	}
	if g.auth == nil {
		g.logf("error", "auth lookup 未注入，跳过禁用 auth=%s", authIndex)
		return
	}

	// Ownership / manual-disable protection.
	current, err := g.findAuth(authIndex)
	if err != nil {
		g.logf("error", "查询 auth-files 失败 auth=%s: %v", authIndex, err)
		return
	}
	if current == nil {
		g.logf("warn", "auth 不存在，跳过禁用 auth=%s", authIndex)
		return
	}

	// Already disabled without our ownership => user_manual.
	existing := g.storeGet(authIndex)
	if current.Disabled {
		if existing != nil && existing.State == StateAutoDisabled && existing.DisableSource == SourcePluginAuto && existing.Owner == Owner && !existing.PreDisabled {
			// Extend cooldown.
			rec := *existing
			rec.RecoverAtMS = match.RecoverAt.UnixMilli()
			rec.Reason = match.Reason
			rec.Signal = match.Signal
			rec.LastEventHash = ev.EventHash
			if rec.Account == "" {
				rec.Account = firstNonEmpty(ev.Account, current.Account)
			}
			if rec.FileName == "" {
				rec.FileName = current.Name
			}
			if err := g.storeUpsert(rec); err != nil {
				g.logf("error", "延长冷却写状态失败 auth=%s: %v", authIndex, err)
				return
			}
			g.logf("info", "已延长 plugin_auto 冷却 auth=%s recover_at=%s", authIndex, match.RecoverAt.Format(time.RFC3339))
			return
		}
		// Mark user manual and never auto-enable.
		rec := AccountRecord{
			AuthIndex:     authIndex,
			FileName:      current.Name,
			Provider:      "xai",
			Account:       firstNonEmpty(ev.Account, current.Account),
			DisableSource: SourceUserManual,
			State:         StateUserManualDisabled,
			DisabledAtMS:  time.Now().UnixMilli(),
			PreDisabled:   true,
			Reason:        "already_disabled_without_plugin_ownership",
			LastEventHash: ev.EventHash,
		}
		_ = g.storeUpsert(rec)
		g.logf("info", "账号已禁用且无本插件所有权，标记 user_manual，跳过 auth=%s", authIndex)
		return
	}

	prev, err := g.auth.SetDisabled(authIndex, true)
	if err != nil {
		g.logf("error", "禁用失败 auth=%s: %v", authIndex, err)
		return
	}
	if prev {
		// Race: became disabled between list and patch.
		rec := AccountRecord{
			AuthIndex:     authIndex,
			FileName:      current.Name,
			Provider:      "xai",
			Account:       firstNonEmpty(ev.Account, current.Account),
			DisableSource: SourceUserManual,
			State:         StateUserManualDisabled,
			DisabledAtMS:  time.Now().UnixMilli(),
			PreDisabled:   true,
			Reason:        "pre_disabled_race",
			LastEventHash: ev.EventHash,
		}
		_ = g.storeUpsert(rec)
		g.logf("info", "禁用竞态：账号此前已禁用，标记 user_manual auth=%s", authIndex)
		return
	}

	nowMS := time.Now().UnixMilli()
	rec := AccountRecord{
		AuthIndex:     authIndex,
		FileName:      current.Name,
		Provider:      "xai",
		Account:       firstNonEmpty(ev.Account, current.Account),
		DisableSource: SourcePluginAuto,
		State:         StateAutoDisabled,
		RecoverAtMS:   match.RecoverAt.UnixMilli(),
		DisabledAtMS:  nowMS,
		PreDisabled:   false,
		Owner:         Owner,
		Reason:        match.Reason,
		Signal:        match.Signal,
		LastEventHash: ev.EventHash,
	}
	if err := g.storeUpsert(rec); err != nil {
		g.logf("error", "写状态失败 auth=%s: %v", authIndex, err)
		return
	}
	g.logf("warn", "xAI 短时额度耗尽，已禁用 auth=%s file=%s recover_at=%s signal=%s",
		authIndex, current.Name, match.RecoverAt.Format(time.RFC3339), match.Signal)
}

// Tick recovers due plugin_auto cooldowns.
func (g *Guard) Tick() {
	cfg := g.Config()
	if !cfg.Enabled {
		return
	}
	if cfg.ManagementURL == "" || cfg.ManagementKey == "" || g.auth == nil {
		return
	}
	due := g.storeDue(time.Now())
	for _, rec := range due {
		// Re-check ownership and current disabled state.
		current, err := g.findAuth(rec.AuthIndex)
		if err != nil {
			g.logf("error", "恢复前查询失败 auth=%s: %v", rec.AuthIndex, err)
			continue
		}
		if current == nil {
			_ = g.storeMarkActive(rec.AuthIndex) // drop missing
			continue
		}
		// Fresh state read.
		live := g.storeGet(rec.AuthIndex)
		if live == nil || live.DisableSource != SourcePluginAuto || live.State != StateAutoDisabled || live.PreDisabled || (live.Owner != "" && live.Owner != Owner) {
			continue
		}
		if !current.Disabled {
			// Already enabled externally; clear our record.
			_ = g.storeMarkActive(rec.AuthIndex)
			g.logf("info", "账号已外部启用，清除 cooldown auth=%s", rec.AuthIndex)
			continue
		}
		if _, err := g.auth.SetDisabled(rec.AuthIndex, false); err != nil {
			g.logf("error", "自动恢复启用失败 auth=%s: %v", rec.AuthIndex, err)
			continue
		}
		_ = g.storeMarkActive(rec.AuthIndex)
		g.logf("info", "xAI 额度重置到达，已自动启用 auth=%s file=%s", rec.AuthIndex, rec.FileName)
	}
}

func (g *Guard) findAuth(authIndex string) (*AuthFile, error) {
	files, err := g.auth.List()
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].AuthIndex == authIndex {
			f := files[i]
			return &f, nil
		}
	}
	return nil, nil
}

func (g *Guard) storeGet(authIndex string) *AccountRecord {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.Get(authIndex)
}

func (g *Guard) storeUpsert(rec AccountRecord) error {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		return fmt.Errorf("store nil")
	}
	return store.Upsert(rec)
}

func (g *Guard) storeMarkActive(authIndex string) error {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.MarkActive(authIndex)
}

func (g *Guard) storeDue(now time.Time) []AccountRecord {
	g.mu.Lock()
	store := g.store
	g.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.DueAutoDisabled(now)
}

func (g *Guard) logf(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if g.logger != nil {
		g.logger.Log(level, msg)
	} else {
		log.Printf("[cpa-xai-quota-guard][%s] %s", level, msg)
	}
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trim(v) != "" {
			return trim(v)
		}
	}
	return ""
}

func headerFromMap(m map[string][]string) http.Header {
	if m == nil {
		return nil
	}
	return http.Header(m)
}