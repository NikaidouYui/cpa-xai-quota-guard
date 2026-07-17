package xaiquota

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Compact ISO-like stamp embedded in many uploaded oauth dumps:
// xai_oauth_user@mail.com_20260712T134620Z.json
var (
	reFileStamp     = regexp.MustCompile(`(?i)_(\d{8}T\d{6}Z)\.json$`)
	reOAuthPrefixed = regexp.MustCompile(`(?i)^xai[_-]oauth[_-](.+)$`)
	reXAIPrefixed   = regexp.MustCompile(`(?i)^xai[_-](.+)$`)
)

// DedupeFile is one credential in a duplicate group.
type DedupeFile struct {
	AuthIndex string `json:"auth_index"`
	FileName  string `json:"file_name,omitempty"`
	Account   string `json:"account,omitempty"`
	Disabled  bool   `json:"disabled"`
	Success   int64  `json:"success,omitempty"`
	Failed    int64  `json:"failed,omitempty"`
	RecencyMS int64  `json:"recency_ms"`
	Identity  string `json:"identity,omitempty"`
	Keep      bool   `json:"keep"`
}

// DedupeGroup is one logical account with multiple credential files.
type DedupeGroup struct {
	Identity string       `json:"identity"`
	Count    int          `json:"count"`
	Keep     DedupeFile   `json:"keep"`
	Delete   []DedupeFile `json:"delete"`
}

// DedupePlan is the full dry-run / execute plan for xAI duplicate cleanup.
type DedupePlan struct {
	Groups       []DedupeGroup `json:"groups"`
	GroupCount   int           `json:"group_count"`
	KeepCount    int           `json:"keep_count"`
	DeleteCount  int           `json:"delete_count"`
	ScannedXAI   int           `json:"scanned_xai"`
	UniqueKeys   int           `json:"unique_keys"`
	SkippedEmpty int           `json:"skipped_no_identity"`
}

// NormalizeAccountIdentity lowercases and trims an account/email key.
func NormalizeAccountIdentity(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// DedupeIdentity returns the logical account key used for duplicate grouping.
// Preference: Account field → identity parsed from filename.
// Empty means "cannot group" (file stays unique).
func DedupeIdentity(f AuthFile) string {
	if acc := NormalizeAccountIdentity(f.Account); acc != "" {
		return acc
	}
	if id, ok := ParseIdentityFromFileName(f.Name); ok {
		return id
	}
	return ""
}

// ParseIdentityFromFileName extracts a stable account-like identity from common
// xAI auth file naming patterns (strips trailing upload timestamps).
func ParseIdentityFromFileName(name string) (string, bool) {
	base := strings.TrimSpace(name)
	if base == "" {
		return "", false
	}
	// strip path
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".json") {
		return "", false
	}
	stem := base[:len(base)-len(".json")]

	// Drop trailing _YYYYMMDDTHHMMSSZ upload stamp first.
	if m := reFileStamp.FindStringSubmatch(base); len(m) == 2 {
		// stem without _STAMP
		if idx := strings.LastIndex(stem, "_"+m[1]); idx > 0 {
			stem = stem[:idx]
		} else if idx := strings.LastIndex(strings.ToLower(stem), "_"+strings.ToLower(m[1])); idx > 0 {
			stem = stem[:idx]
		}
	}

	// Only known xAI credential filename shapes (avoid grouping arbitrary json).
	id := ""
	if m := reOAuthPrefixed.FindStringSubmatch(stem); len(m) == 2 {
		id = m[1]
	} else if m := reXAIPrefixed.FindStringSubmatch(stem); len(m) == 2 {
		id = m[1]
	} else {
		return "", false
	}
	id = NormalizeAccountIdentity(id)
	if id == "" || id == "oauth" || id == "api" || id == "key" {
		return "", false
	}
	// Require something account-like: email / domain / multi-segment token.
	if !strings.Contains(id, "@") && !strings.Contains(id, ".") && !strings.Contains(id, "_") && len(id) < 4 {
		return "", false
	}
	return id, true
}

// FileNameRecencyMS returns upload-time recency from filename stamp when present.
func FileNameRecencyMS(name string) int64 {
	m := reFileStamp.FindStringSubmatch(name)
	if len(m) != 2 {
		return 0
	}
	ts, err := time.Parse("20060102T150405Z", strings.ToUpper(m[1]))
	if err != nil {
		return 0
	}
	return ts.UnixMilli()
}

// CredentialRecencyMS ranks "newest upload":
// 1) filename ISO stamp  2) modtime  3) 0
func CredentialRecencyMS(f AuthFile) int64 {
	if ms := FileNameRecencyMS(f.Name); ms > 0 {
		return ms
	}
	if f.ModTimeMS > 0 {
		return f.ModTimeMS
	}
	return 0
}

// betterDedupeKeep returns true if a should be kept over b (newer / safer tie-break).
func betterDedupeKeep(a, b DedupeFile) bool {
	if a.RecencyMS != b.RecencyMS {
		return a.RecencyMS > b.RecencyMS
	}
	// Prefer enabled credential when upload time ties.
	if a.Disabled != b.Disabled {
		return !a.Disabled
	}
	if a.Success != b.Success {
		return a.Success > b.Success
	}
	// Stable: later lexicographic filename (often newer stamp still differs).
	if a.FileName != b.FileName {
		return a.FileName > b.FileName
	}
	return a.AuthIndex > b.AuthIndex
}

func toDedupeFile(f AuthFile, identity string) DedupeFile {
	return DedupeFile{
		AuthIndex: f.AuthIndex,
		FileName:  f.Name,
		Account:   firstNonEmpty(f.Account, identity),
		Disabled:  f.Disabled,
		Success:   f.Success,
		Failed:    f.Failed,
		RecencyMS: CredentialRecencyMS(f),
		Identity:  identity,
	}
}

// PlanDedupeXAI scans xAI auth files and builds a keep-newest plan.
// Non-xAI providers are ignored. Files without identity are never grouped.
func PlanDedupeXAI(files []AuthFile) DedupePlan {
	plan := DedupePlan{}
	byKey := map[string][]DedupeFile{}
	for _, f := range files {
		if !IsXAIProvider(f.Provider, "") {
			continue
		}
		plan.ScannedXAI++
		id := DedupeIdentity(f)
		if id == "" {
			plan.SkippedEmpty++
			continue
		}
		byKey[id] = append(byKey[id], toDedupeFile(f, id))
	}
	plan.UniqueKeys = len(byKey)

	keys := make([]string, 0, len(byKey))
	for k, list := range byKey {
		if len(list) < 2 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		list := byKey[k]
		// pick keep
		keepIdx := 0
		for i := 1; i < len(list); i++ {
			if betterDedupeKeep(list[i], list[keepIdx]) {
				keepIdx = i
			}
		}
		keep := list[keepIdx]
		keep.Keep = true
		del := make([]DedupeFile, 0, len(list)-1)
		for i := range list {
			if i == keepIdx {
				continue
			}
			d := list[i]
			d.Keep = false
			del = append(del, d)
		}
		// stable order of deletes: oldest first
		sort.SliceStable(del, func(i, j int) bool {
			if del[i].RecencyMS != del[j].RecencyMS {
				return del[i].RecencyMS < del[j].RecencyMS
			}
			return del[i].FileName < del[j].FileName
		})
		g := DedupeGroup{
			Identity: k,
			Count:    len(list),
			Keep:     keep,
			Delete:   del,
		}
		plan.Groups = append(plan.Groups, g)
		plan.KeepCount++
		plan.DeleteCount += len(del)
	}
	plan.GroupCount = len(plan.Groups)
	return plan
}

// DedupeExecuteResult is the outcome of applying a plan.
type DedupeExecuteResult struct {
	Plan         DedupePlan    `json:"plan"`
	Deleted      []DedupeFile  `json:"deleted"`
	Failed       []DedupeError `json:"failed,omitempty"`
	DeletedCount int           `json:"deleted_count"`
	FailedCount  int           `json:"failed_count"`
	DryRun       bool          `json:"dry_run"`
}

// DedupeError records one failed delete.
type DedupeError struct {
	AuthIndex string `json:"auth_index"`
	FileName  string `json:"file_name,omitempty"`
	Error     string `json:"error"`
}

// ExecuteDedupeXAI deletes older duplicates in plan, keeping the newest of each group.
// dryRun=true only returns the plan without mutating CPA.
func (g *Guard) ExecuteDedupeXAI(dryRun bool) (DedupeExecuteResult, error) {
	res := DedupeExecuteResult{DryRun: dryRun}
	if g == nil || g.auth == nil {
		return res, fmt.Errorf("auth lookup not configured")
	}
	files, err := g.auth.List()
	if err != nil {
		return res, err
	}
	plan := PlanDedupeXAI(files)
	res.Plan = plan
	if dryRun || plan.DeleteCount == 0 {
		return res, nil
	}

	now := time.Now().UnixMilli()
	for _, group := range plan.Groups {
		for _, d := range group.Delete {
			if d.AuthIndex == "" {
				res.Failed = append(res.Failed, DedupeError{
					AuthIndex: d.AuthIndex,
					FileName:  d.FileName,
					Error:     "empty auth_index",
				})
				continue
			}
			if err := g.auth.Delete(d.AuthIndex); err != nil {
				res.Failed = append(res.Failed, DedupeError{
					AuthIndex: d.AuthIndex,
					FileName:  d.FileName,
					Error:     err.Error(),
				})
				g.logf("error", "去重删除失败 auth=%s file=%s: %v", d.AuthIndex, d.FileName, err)
				continue
			}
			_ = g.storeRemove(d.AuthIndex)
			if g.store != nil {
				_ = g.store.AppendDelete(DeleteEvent{
					AuthIndex:   d.AuthIndex,
					FileName:    d.FileName,
					Account:     firstNonEmpty(d.Account, group.Identity),
					Provider:    "xai",
					Reason:      "duplicate_credential keep=" + group.Keep.FileName,
					DeletedAtMS: now,
				})
			}
			g.appendAction(ActionEvent{
				TimeMS:    now,
				Action:    "delete",
				Source:    "dedupe",
				AuthIndex: d.AuthIndex,
				FileName:  d.FileName,
				Account:   firstNonEmpty(d.Account, group.Identity),
				Signal:    "duplicate",
				Reason:    "duplicate_credential keep=" + group.Keep.FileName,
				Provider:  "xai",
			})
			g.logf("warn", "去重删除旧凭证 auth=%s file=%s identity=%s keep=%s",
				d.AuthIndex, d.FileName, group.Identity, group.Keep.FileName)
			res.Deleted = append(res.Deleted, d)
		}
	}
	res.DeletedCount = len(res.Deleted)
	res.FailedCount = len(res.Failed)
	if res.DeletedCount > 0 {
		g.NotifyWebhook("dedupe_delete", map[string]any{
			"deleted_count": res.DeletedCount,
			"failed_count":  res.FailedCount,
			"group_count":   plan.GroupCount,
			"kept":          plan.KeepCount,
		})
	}
	return res, nil
}
