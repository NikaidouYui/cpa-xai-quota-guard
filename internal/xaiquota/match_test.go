package xaiquota

import (
	"net/http"
	"testing"
	"time"
)

func TestMatchXAIRateLimitWithRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := http.Header{}
	h.Set("Retry-After", "60")
	got, ok := MatchShortWindowQuota(MatchInput{
		Provider:        "xai",
		Failed:          true,
		StatusCode:      429,
		Body:            `{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached for requests per minute","type":"tokens"}}`,
		ResponseHeaders: h,
		Now:             now,
		MaxResetSeconds: 86400,
	})
	if !ok {
		t.Fatal("expected match")
	}
	if !got.RecoverAt.Equal(now.Add(60 * time.Second)) {
		t.Fatalf("recover_at = %v", got.RecoverAt)
	}
}

func TestMatchXAIRateLimitWithBodyRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got, ok := MatchShortWindowQuota(MatchInput{
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"message":"Too many requests","type":"tokens","code":"rate_limit_exceeded","retry_after":120}}`,
		Now:        now,
	})
	if !ok {
		t.Fatal("expected match")
	}
	if !got.RecoverAt.Equal(now.Add(120 * time.Second)) {
		t.Fatalf("recover_at = %v", got.RecoverAt)
	}
}

func TestMatchIgnoresNonXAI(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	_, ok := MatchShortWindowQuota(MatchInput{
		Provider:   "codex",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"type":"usage_limit_reached","resets_in_seconds":60}}`,
		Now:        now,
	})
	if ok {
		t.Fatal("codex must not match xAI plugin")
	}
}

func TestMatchIgnoresAuthErrors(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	_, ok := MatchShortWindowQuota(MatchInput{
		Provider:   "xai",
		Failed:     true,
		StatusCode: 401,
		Body:       `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`,
		Now:        now,
	})
	if ok {
		t.Fatal("401 must be ignored")
	}
}

func TestMatchIgnoresInsufficientQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := http.Header{}
	h.Set("Retry-After", "60")
	_, ok := MatchShortWindowQuota(MatchInput{
		Provider:        "xai",
		Failed:          true,
		StatusCode:      429,
		Body:            `{"error":{"message":"You exceeded your current quota, please check your plan and billing details","code":"insufficient_quota"}}`,
		ResponseHeaders: h,
		Now:             now,
	})
	if ok {
		t.Fatal("insufficient_quota must be ignored")
	}
}

func TestMatchRequiresResetTime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	_, ok := MatchShortWindowQuota(MatchInput{
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached for requests"}}`,
		Now:        now,
	})
	if ok {
		t.Fatal("missing reset time must not match")
	}
}

func TestMatchXAIHeaderReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := http.Header{}
	h.Set("x-ratelimit-remaining-requests", "0")
	h.Set("x-ratelimit-reset-requests", "90")
	got, ok := MatchShortWindowQuota(MatchInput{
		Provider:        "xai",
		Failed:          true,
		StatusCode:      429,
		Body:            `{"error":{"message":"Too Many Requests"}}`,
		ResponseHeaders: h,
		Now:             now,
	})
	if !ok {
		t.Fatal("expected header-based match")
	}
	if !got.RecoverAt.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("recover_at = %v", got.RecoverAt)
	}
}

func TestMatchRejectsCodexUsageLimitOnXAIProvider(t *testing.T) {
	// Codex-style body must not be the primary xAI signal unless it also has
	// real xAI rate-limit fields/time. Pure usage_limit_reached is ignored.
	now := time.Unix(1_700_000_000, 0)
	_, ok := MatchShortWindowQuota(MatchInput{
		Provider:   "xai",
		Failed:     true,
		StatusCode: 429,
		Body:       `{"error":{"type":"usage_limit_reached","resets_in_seconds":30}}`,
		Now:        now,
	})
	if ok {
		t.Fatal("codex usage_limit_reached must not be treated as xAI short-window signal")
	}
}

func TestIsXAIProvider(t *testing.T) {
	if !IsXAIProvider("xai", "") {
		t.Fatal("xai provider")
	}
	if !IsXAIProvider("", "xai") {
		t.Fatal("auth_type fallback")
	}
	if IsXAIProvider("openai", "") {
		t.Fatal("openai must fail")
	}
	if IsXAIProvider("codex", "") {
		t.Fatal("codex must fail")
	}
}