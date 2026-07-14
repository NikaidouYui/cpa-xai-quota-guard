package xaiquota

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StateActive             = "active"
	StateAutoDisabled       = "auto_disabled"
	StateUserManualDisabled = "user_manual_disabled"

	SourceNone       = "none"
	SourcePluginAuto = "plugin_auto"
	SourceUserManual = "user_manual"

	stateVersion = 2
)

const (
	actionBodyMax   = 4096
	actionReasonMax = 240
	actionHeaderMax = 200
)

type AccountRecord struct {
	AuthIndex      string `json:"auth_index"`
	FileName       string `json:"file_name,omitempty"`
	Provider       string `json:"provider,omitempty"`
	Account        string `json:"account,omitempty"`
	DisableSource  string `json:"disable_source"`
	State          string `json:"state"`
	RecoverAtMS    int64  `json:"recover_at_ms,omitempty"`
	DisabledAtMS   int64  `json:"disabled_at_ms,omitempty"`
	PreDisabled    bool   `json:"pre_disabled,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Signal         string `json:"signal,omitempty"`
	LastProbeModel string `json:"last_probe_model,omitempty"`
	LastEventHash  string `json:"last_event_hash,omitempty"`
	UpdatedAtMS    int64  `json:"updated_at_ms,omitempty"`
}

type DeleteEvent struct {
	AuthIndex   string `json:"auth_index"`
	FileName    string `json:"file_name,omitempty"`
	Account     string `json:"account,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Reason      string `json:"reason,omitempty"`
	DeletedAtMS int64  `json:"deleted_at_ms"`
}

type ActionEvent struct {
	TimeMS    int64             `json:"time_ms"`
	Action    string            `json:"action"`
	Source    string            `json:"source,omitempty"`
	AuthIndex string            `json:"auth_index,omitempty"`
	FileName  string            `json:"file_name,omitempty"`
	Account   string            `json:"account,omitempty"`
	HTTPCode  int               `json:"http_code,omitempty"`
	Signal    string            `json:"signal,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	AuthType  string            `json:"auth_type,omitempty"`
	Model     string            `json:"model,omitempty"`
	Body      string            `json:"body,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	EventHash string            `json:"event_hash,omitempty"`
}

type PatrolLogEntry struct {
	TimeMS    int64  `json:"time_ms"`
	AuthIndex string `json:"auth_index,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	Account   string `json:"account,omitempty"`
	Action    string `json:"action,omitempty"`
	Reason    string `json:"reason,omitempty"`
	HTTPCode  int    `json:"http_code,omitempty"`
}

type PatrolSnapshot struct {
	Running         bool             `json:"running"`
	StartedAtMS     int64            `json:"started_at_ms,omitempty"`
	CompletedAtMS   int64            `json:"completed_at_ms,omitempty"`
	TotalCandidates int              `json:"total_candidates"`
	TotalProbed     int              `json:"total_probed"`
	TotalDeleted    int              `json:"total_deleted"`
	TotalErrors     int              `json:"total_errors"`
	TotalAlive      int              `json:"total_alive"`
	TotalSkipped    int              `json:"total_skipped"`
	TotalCooldown   int              `json:"total_cooldown"`
	Total429CD      int              `json:"total_429_cooldown"`
	TotalSpendCD    int              `json:"total_402_cooldown"`
	TotalReenabled  int              `json:"total_reenabled"`
	ByHTTP          map[string]int   `json:"by_http,omitempty"`
	ByAction        map[string]int   `json:"by_action,omitempty"`
	Workers         int              `json:"workers,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	Scope           string           `json:"scope,omitempty"`
	RecentLog       []PatrolLogEntry `json:"recent_log,omitempty"`
	SavedAtMS       int64            `json:"saved_at_ms,omitempty"`
}

type Store struct {
	mu      sync.Mutex
	path    string
	dbPath  string
	db      *sql.DB
	Usage   *UsageStats
	Updated int64
	Version int
}

type legacyState struct {
	Version       int                       `json:"version"`
	Updated       int64                     `json:"updated_at_ms"`
	Accounts      map[string]*AccountRecord `json:"accounts"`
	Usage         *UsageStats               `json:"usage,omitempty"`
	DeleteHistory []DeleteEvent             `json:"delete_history,omitempty"`
	ActionHistory []ActionEvent             `json:"action_history,omitempty"`
	LastPatrol    *PatrolSnapshot           `json:"last_patrol,omitempty"`
}

func NewStore(path string) (*Store, error) {
	dbPath, legacyPath := sqlitePaths(path)
	existed := false
	if dbPath != ":memory:" {
		_, statErr := os.Stat(dbPath)
		existed = statErr == nil
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{path: path, dbPath: dbPath, db: db, Version: stateVersion, Usage: &UsageStats{}}
	if !existed && legacyPath != "" {
		if err := importLegacy(s, legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = db.Close()
			_ = os.Remove(dbPath)
			_ = os.Remove(dbPath + "-wal")
			_ = os.Remove(dbPath + "-shm")
			return nil, err
		}
	}
	if err := s.loadUsageLocked(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func sqlitePaths(path string) (dbPath, legacyPath string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ":memory:", ""
	}
	if strings.HasSuffix(strings.ToLower(path), ".sqlite") || strings.HasSuffix(strings.ToLower(path), ".db") {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		return path, base + ".json"
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return strings.TrimSuffix(path, filepath.Ext(path)) + ".sqlite", path
	}
	return path + ".sqlite", path
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS accounts (auth_index TEXT PRIMARY KEY, data BLOB NOT NULL, updated_at_ms INTEGER NOT NULL, state TEXT NOT NULL, disable_source TEXT NOT NULL, recover_at_ms INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS usage_meta (id INTEGER PRIMARY KEY CHECK (id=1), data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS usage_auth (auth_index TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS quota_auth (auth_index TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS delete_history (id INTEGER PRIMARY KEY AUTOINCREMENT, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS action_history (id INTEGER PRIMARY KEY AUTOINCREMENT, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS patrol_snapshot (id INTEGER PRIMARY KEY CHECK (id=1), data BLOB NOT NULL);
`)
	return err
}

func importLegacy(s *Store, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	var old legacyState
	if err := json.Unmarshal(raw, &old); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, rec := range old.Accounts {
		if rec == nil {
			continue
		}
		if rec.AuthIndex == "" {
			rec.AuthIndex = k
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO accounts(auth_index,data,updated_at_ms,state,disable_source,recover_at_ms) VALUES(?,?,?,?,?,?)`, rec.AuthIndex, mustJSON(rec), rec.UpdatedAtMS, rec.State, rec.DisableSource, rec.RecoverAtMS); err != nil {
			return err
		}
	}
	if old.Usage != nil {
		if err := putUsageTx(tx, old.Usage); err != nil {
			return err
		}
	}
	for _, ev := range old.DeleteHistory {
		if err := putJSON(tx, `INSERT INTO delete_history(data) VALUES(?)`, ev); err != nil {
			return err
		}
	}
	for _, ev := range old.ActionHistory {
		if err := putJSON(tx, `INSERT INTO action_history(data) VALUES(?)`, ev); err != nil {
			return err
		}
	}
	if old.LastPatrol != nil {
		if err := putJSON(tx, `INSERT OR REPLACE INTO patrol_snapshot(id,data) VALUES(1,?)`, old.LastPatrol); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Keep a recoverable copy; never remove the original state silently.
	_ = os.Rename(path, path+".migrated.bak")
	return nil
}

func putJSON(tx *sql.Tx, query string, args ...any) error {
	if len(args) != 1 {
		return errors.New("putJSON requires one value")
	}
	args = []any{mustJSON(args[0])}
	_, err := tx.Exec(query, args...)
	return err
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func putUsageTx(tx *sql.Tx, st *UsageStats) error {
	st = EnsureUsageStats(st)
	meta := *st
	meta.UsageByAuth = nil
	meta.QuotaByAuth = nil
	if _, err := tx.Exec(`INSERT OR REPLACE INTO usage_meta(id,data) VALUES(1,?)`, mustJSON(&meta)); err != nil {
		return err
	}
	for k, v := range st.UsageByAuth {
		if v != nil {
			if _, err := tx.Exec(`INSERT OR REPLACE INTO usage_auth(auth_index,data) VALUES(?,?)`, k, mustJSON(v)); err != nil {
				return err
			}
		}
	}
	for k, v := range st.QuotaByAuth {
		if v != nil {
			if _, err := tx.Exec(`INSERT OR REPLACE INTO quota_auth(auth_index,data) VALUES(?,?)`, k, mustJSON(v)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}
func (s *Store) DBPath() string {
	if s == nil {
		return ""
	}
	return s.dbPath
}

func (s *Store) Get(authIndex string) *AccountRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw []byte
	if err := s.db.QueryRow(`SELECT data FROM accounts WHERE auth_index=?`, authIndex).Scan(&raw); err != nil {
		return nil
	}
	var rec AccountRecord
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	return &rec
}

func (s *Store) Snapshot() map[string]AccountRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]AccountRecord{}
	rows, err := s.db.Query(`SELECT data FROM accounts`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var rec AccountRecord
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &rec) == nil {
			out[rec.AuthIndex] = rec
		}
	}
	return out
}

func (s *Store) Upsert(rec AccountRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.UpdatedAtMS = time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO accounts(auth_index,data,updated_at_ms,state,disable_source,recover_at_ms) VALUES(?,?,?,?,?,?)`, rec.AuthIndex, mustJSON(&rec), rec.UpdatedAtMS, rec.State, rec.DisableSource, rec.RecoverAtMS)
	return err
}

func (s *Store) MarkActive(authIndex string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw []byte
	if err := s.db.QueryRow(`SELECT data FROM accounts WHERE auth_index=?`, authIndex).Scan(&raw); err != nil {
		return nil
	}
	var rec AccountRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return err
	}
	rec.State, rec.DisableSource, rec.RecoverAtMS, rec.DisabledAtMS, rec.PreDisabled, rec.Owner, rec.Reason, rec.Signal = StateActive, SourceNone, 0, 0, false, "", "", ""
	rec.UpdatedAtMS = time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO accounts(auth_index,data,updated_at_ms,state,disable_source,recover_at_ms) VALUES(?,?,?,?,?,?)`, authIndex, mustJSON(&rec), rec.UpdatedAtMS, rec.State, rec.DisableSource, rec.RecoverAtMS)
	return err
}

func (s *Store) Remove(authIndex string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM accounts WHERE auth_index=?`, authIndex)
	return err
}

func (s *Store) DueAutoDisabled(now time.Time) []AccountRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT data FROM accounts WHERE state=? AND disable_source=? AND recover_at_ms>0 AND recover_at_ms<=?`, StateAutoDisabled, SourcePluginAuto, now.UnixMilli())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []AccountRecord
	for rows.Next() {
		var raw []byte
		var rec AccountRecord
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &rec) == nil && !rec.PreDisabled && (rec.Owner == "" || rec.Owner == Owner) {
			out = append(out, rec)
		}
	}
	return out
}

func (s *Store) AppendDelete(ev DeleteEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.DeletedAtMS == 0 {
		ev.DeletedAtMS = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO delete_history(data) VALUES(?)`, mustJSON(&ev))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM delete_history WHERE id NOT IN (SELECT id FROM delete_history ORDER BY id DESC LIMIT 200)`)
	return err
}

func (s *Store) ListDeletes(limit int) []DeleteEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT data FROM delete_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DeleteEvent
	for rows.Next() {
		var raw []byte
		var ev DeleteEvent
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

func (s *Store) AppendAction(ev ActionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ev.TimeMS == 0 {
		ev.TimeMS = time.Now().UnixMilli()
	}
	ev.Reason = truncate(ev.Reason, actionReasonMax)
	ev.Body = truncate(ev.Body, actionBodyMax)
	if len(ev.Headers) > 0 {
		h := map[string]string{}
		for k, v := range ev.Headers {
			k = strings.TrimSpace(k)
			v = truncate(v, actionHeaderMax)
			if k != "" && v != "" {
				h[k] = v
			}
		}
		if len(h) == 0 {
			ev.Headers = nil
		} else {
			ev.Headers = h
		}
	}
	_, err := s.db.Exec(`INSERT INTO action_history(data) VALUES(?)`, mustJSON(&ev))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM action_history WHERE id NOT IN (SELECT id FROM action_history ORDER BY id DESC LIMIT 500)`)
	return err
}

func (s *Store) ListActions(limit int) []ActionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT data FROM action_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ActionEvent
	for rows.Next() {
		var raw []byte
		var ev ActionEvent
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

func (s *Store) SaveLastPatrol(snap PatrolSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snap.SavedAtMS == 0 {
		snap.SavedAtMS = time.Now().UnixMilli()
	}
	if len(snap.RecentLog) > 300 {
		snap.RecentLog = snap.RecentLog[len(snap.RecentLog)-300:]
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO patrol_snapshot(id,data) VALUES(1,?)`, mustJSON(&snap))
	return err
}

func (s *Store) GetLastPatrol() *PatrolSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw []byte
	if s.db.QueryRow(`SELECT data FROM patrol_snapshot WHERE id=1`).Scan(&raw) != nil {
		return nil
	}
	var snap PatrolSnapshot
	if json.Unmarshal(raw, &snap) != nil {
		return nil
	}
	return &snap
}

func (s *Store) loadUsageLocked() error {
	s.Usage = &UsageStats{}
	var raw []byte
	if err := s.db.QueryRow(`SELECT data FROM usage_meta WHERE id=1`).Scan(&raw); err == nil {
		if err = json.Unmarshal(raw, s.Usage); err != nil {
			return err
		}
	}
	s.Usage = EnsureUsageStats(s.Usage)
	rows, err := s.db.Query(`SELECT auth_index,data FROM usage_auth`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var b []byte
		var v AccountUsageSnapshot
		if rows.Scan(&k, &b) == nil && json.Unmarshal(b, &v) == nil {
			s.Usage.UsageByAuth[k] = &v
		}
	}
	rows.Close()
	rows, err = s.db.Query(`SELECT auth_index,data FROM quota_auth`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k string
		var b []byte
		var v AccountQuotaSnapshot
		if rows.Scan(&k, &b) == nil && json.Unmarshal(b, &v) == nil {
			s.Usage.QuotaByAuth[k] = &v
		}
	}
	rows.Close()
	return nil
}

func (s *Store) persistUsageMetaLocked() error {
	st := EnsureUsageStats(s.Usage)
	meta := *st
	meta.UsageByAuth = nil
	meta.QuotaByAuth = nil
	_, err := s.db.Exec(`INSERT OR REPLACE INTO usage_meta(id,data) VALUES(1,?)`, mustJSON(&meta))
	return err
}

func (s *Store) persistUsageAuth(authIndex string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if authIndex == "" {
		return nil
	}
	u := s.Usage.UsageByAuth[authIndex]
	if u == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO usage_auth(auth_index,data) VALUES(?,?)`, authIndex, mustJSON(u))
	return err
}
func (s *Store) persistUsageQuota(authIndex string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if authIndex == "" {
		return nil
	}
	q := s.Usage.QuotaByAuth[authIndex]
	if q == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO quota_auth(auth_index,data) VALUES(?,?)`, authIndex, mustJSON(q))
	return err
}
func (s *Store) persistUsageAllAuth() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for k, v := range s.Usage.UsageByAuth {
		if v != nil {
			if _, err = tx.Exec(`INSERT OR REPLACE INTO usage_auth(auth_index,data) VALUES(?,?)`, k, mustJSON(v)); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
