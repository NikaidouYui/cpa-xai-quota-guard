package xaiquota

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Owner is persisted on cooldowns owned by this plugin.
const Owner = "cpa_xai_quota_plugin"

// MatchInput is the minimal failure snapshot needed for xAI short-window matching.
type MatchInput struct {
	Provider        string
	AuthType        string
	Failed          bool
	StatusCode      int
	Body            string
	ResponseHeaders http.Header
	Now             time.Time
	MaxResetSeconds float64
}

// MatchResult is returned only when the event is a clear xAI short-window limit
// with a parseable future recover time.
type MatchResult struct {
	RecoverAt time.Time
	Reason    string
	Signal    string
}

// MatchShortWindowQuota returns a result only for strict xAI short-window
// rate-limit failures. Network/auth/ban/monthly-quota/generic errors are skipped.
// Time parse failure => no match (caller must not disable).
func MatchShortWindowQuota(in MatchInput) (MatchResult, bool) {
	if !in.Failed {
		return MatchResult{}, false
	}
	if !IsXAIProvider(in.Provider, in.AuthType) {
		return MatchResult{}, false
	}
	if in.StatusCode != http.StatusTooManyRequests {
		return MatchResult{}, false
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	maxReset := in.MaxResetSeconds
	if maxReset <= 0 {
		maxReset = 86400
	}

	body := strings.TrimSpace(in.Body)
	if isExcludedFailure(body) {
		return MatchResult{}, false
	}

	signal, ok := detectShortWindowSignal(body, in.ResponseHeaders)
	if !ok {
		return MatchResult{}, false
	}

	recoverAt, ok := parseRecoverAt(body, in.ResponseHeaders, now, maxReset)
	if !ok {
		return MatchResult{}, false
	}
	if !recoverAt.After(now) {
		return MatchResult{}, false
	}
	if recoverAt.Sub(now).Seconds() > maxReset {
		return MatchResult{}, false
	}

	return MatchResult{
		RecoverAt: recoverAt,
		Reason:    truncate(body, 240),
		Signal:    signal,
	}, true
}

// IsXAIProvider accepts only xAI-family provider identifiers.
func IsXAIProvider(provider, authType string) bool {
	p := normalizeToken(provider)
	if p == "xai" || p == "x_ai" || p == "x-ai" {
		return true
	}
	a := normalizeToken(authType)
	return a == "xai" || a == "x_ai" || a == "x-ai"
}

func normalizeToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func isExcludedFailure(body string) bool {
	lower := strings.ToLower(body)
	excludes := []string{
		"unauthorized",
		"invalid_api_key",
		"invalid api key",
		"incorrect api key",
		"permission_denied",
		"permission denied",
		"forbidden",
		"access denied",
		"banned",
		"suspended",
		"account_deactivated",
		"insufficient_quota",
		"insufficient quota",
		"payment_required",
		"payment required",
		"billing hard limit",
		"credit balance is too low",
		"monthly limit",
		"monthly quota",
		"quota exceeded for the month",
		"spend limit",
	}
	for _, key := range excludes {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

func detectShortWindowSignal(body string, headers http.Header) (string, bool) {
	if code, typ, msg := extractErrorFields(body); code != "" || typ != "" || msg != "" {
		if isRateLimitCode(code) {
			return "body.error.code=" + code, true
		}
		if isRateLimitType(typ) {
			return "body.error.type=" + typ, true
		}
		if isRateLimitMessage(msg) {
			return "body.error.message", true
		}
	}
	if isRateLimitMessage(body) {
		return "body.message", true
	}
	if headers != nil {
		if headerValueEqualsZero(headers, "x-ratelimit-remaining-requests") ||
			headerValueEqualsZero(headers, "x-ratelimit-remaining-tokens") {
			return "header.x-ratelimit-remaining=0", true
		}
		if headerHas(headers, "x-ratelimit-reset-requests") ||
			headerHas(headers, "x-ratelimit-reset-tokens") ||
			headerHas(headers, "x-ratelimit-reset") {
			return "header.x-ratelimit-reset", true
		}
	}
	return "", false
}

func isRateLimitCode(code string) bool {
	switch normalizeKey(code) {
	case "rate_limit_exceeded", "rate_limit", "too_many_requests", "ratelimitexceeded":
		return true
	default:
		return false
	}
}

func isRateLimitType(typ string) bool {
	switch normalizeKey(typ) {
	case "tokens", "requests", "rate_limit_error", "rate_limit_exceeded", "rate_limit", "too_many_requests":
		return true
	default:
		return false
	}
}

func isRateLimitMessage(msg string) bool {
	lower := strings.ToLower(msg)
	needles := []string{
		"rate limit",
		"rate_limit",
		"rate-limit",
		"too many requests",
		"tokens per minute",
		"requests per minute",
		"tokens per hour",
		"requests per hour",
		"tokens per day",
		"requests per day",
		"rate_limit_exceeded",
	}
	// Require token-like TPM/RPM only with surrounding rate context to reduce false positives.
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	if (strings.Contains(lower, "tpm") || strings.Contains(lower, "rpm")) &&
		(strings.Contains(lower, "limit") || strings.Contains(lower, "exceed") || strings.Contains(lower, "rate")) {
		return true
	}
	return false
}

func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func extractErrorFields(body string) (code, typ, msg string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", ""
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		if i := strings.Index(body, "{"); i >= 0 {
			_ = json.Unmarshal([]byte(body[i:]), &root)
		}
	}
	if root == nil {
		return "", "", ""
	}
	walkJSON(root, func(m map[string]any) bool {
		if c := stringField(m, "code"); c != "" && code == "" {
			code = c
		}
		if t := stringField(m, "type"); t != "" && typ == "" {
			typ = t
		}
		if message := stringField(m, "message"); message != "" && msg == "" {
			msg = message
		}
		return code != "" && typ != "" && msg != ""
	})
	return code, typ, msg
}

func walkJSON(v any, fn func(map[string]any) bool) {
	switch t := v.(type) {
	case map[string]any:
		if fn(t) {
			return
		}
		for _, child := range t {
			walkJSON(child, fn)
		}
	case []any:
		for _, child := range t {
			walkJSON(child, fn)
		}
	}
}

func stringField(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	switch t := raw.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(stringify(t))
	}
}

func parseRecoverAt(body string, headers http.Header, now time.Time, maxReset float64) (time.Time, bool) {
	if headers != nil {
		if at, ok := parseRetryAfter(headers.Get("Retry-After"), now); ok {
			return at, true
		}
		for _, key := range []string{
			"x-ratelimit-reset-requests",
			"x-ratelimit-reset-tokens",
			"x-ratelimit-reset",
		} {
			if at, ok := parseResetHeaderValue(headers.Get(key), now, maxReset); ok {
				return at, true
			}
		}
	}
	if at, ok := parseResetFromBody(body, now, maxReset); ok {
		return at, true
	}
	return time.Time{}, false
}

func parseRetryAfter(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
		return now.Add(time.Duration(sec * float64(time.Second))), true
	}
	for _, layout := range []string{time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseResetHeaderValue(raw string, now time.Time, maxReset float64) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return now.Add(d), true
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil && n > 0 {
		return numberToResetTime(n, now, maxReset, false)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func parseResetFromBody(body string, now time.Time, maxReset float64) (time.Time, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return time.Time{}, false
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		if i := strings.Index(body, "{"); i >= 0 {
			_ = json.Unmarshal([]byte(body[i:]), &root)
		}
	}
	if root == nil {
		return time.Time{}, false
	}

	relKeys := []string{"retry_after", "retryAfter", "resets_in_seconds", "resetsInSeconds", "retry_after_seconds"}
	absKeys := []string{"reset_at", "resets_at", "resetsAt", "resetAt", "retry_at", "retryAt"}
	msKeys := []string{"retry_after_ms", "retryAfterMs", "reset_at_ms", "resets_at_ms"}

	var found time.Time
	ok := false
	walkJSON(root, func(m map[string]any) bool {
		for _, k := range msKeys {
			if raw, exists := m[k]; exists {
				if n, good := toFloat(raw); good && n > 0 {
					found = now.Add(time.Duration(n) * time.Millisecond)
					ok = true
					return true
				}
			}
		}
		for _, k := range relKeys {
			if raw, exists := m[k]; exists {
				if at, good := valueToResetTime(raw, now, maxReset, true); good {
					found = at
					ok = true
					return true
				}
			}
		}
		for _, k := range absKeys {
			if raw, exists := m[k]; exists {
				if at, good := valueToResetTime(raw, now, maxReset, false); good {
					found = at
					ok = true
					return true
				}
			}
		}
		return false
	})
	return found, ok
}

func valueToResetTime(raw any, now time.Time, maxReset float64, relative bool) (time.Time, bool) {
	if raw == nil {
		return time.Time{}, false
	}
	switch t := raw.(type) {
	case float64:
		return numberToResetTime(t, now, maxReset, relative)
	case json.Number:
		n, err := t.Float64()
		if err != nil {
			return time.Time{}, false
		}
		return numberToResetTime(n, now, maxReset, relative)
	case string:
		s := strings.TrimSpace(t)
		if s == "" || strings.EqualFold(s, "null") {
			return time.Time{}, false
		}
		if !relative {
			if at, err := time.Parse(time.RFC3339, s); err == nil {
				return at, true
			}
			if at, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return at, true
			}
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return numberToResetTime(n, now, maxReset, relative)
		}
		return time.Time{}, false
	default:
		return valueToResetTime(stringify(t), now, maxReset, relative)
	}
}

func numberToResetTime(n float64, now time.Time, maxReset float64, relative bool) (time.Time, bool) {
	if n <= 0 {
		return time.Time{}, false
	}
	if relative {
		return now.Add(time.Duration(n * float64(time.Second))), true
	}
	if n > 1_000_000_000_000 {
		return time.UnixMilli(int64(n)), true
	}
	if n > 1_000_000_000 {
		return time.Unix(int64(n), 0), true
	}
	if n <= maxReset {
		return now.Add(time.Duration(n * float64(time.Second))), true
	}
	return time.Time{}, false
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	default:
		n, err := strconv.ParseFloat(strings.TrimSpace(stringify(t)), 64)
		return n, err == nil
	}
}

func headerHas(h http.Header, key string) bool {
	return strings.TrimSpace(h.Get(key)) != ""
}

func headerValueEqualsZero(h http.Header, key string) bool {
	v := strings.TrimSpace(h.Get(key))
	if v == "" {
		return false
	}
	n, err := strconv.ParseFloat(v, 64)
	return err == nil && n == 0
}

func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}