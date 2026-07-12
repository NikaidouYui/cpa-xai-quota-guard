package xaiquota

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CPAMPClient pulls monitoring analytics for backfill / deep-link helpers.
type CPAMPClient struct {
	BaseURL   string
	AdminKey  string
	HTTP      *http.Client
}

func NewCPAMPClient(baseURL, adminKey string) *CPAMPClient {
	return &CPAMPClient{
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AdminKey: strings.TrimSpace(adminKey),
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *CPAMPClient) Enabled() bool {
	return c != nil && c.BaseURL != "" && c.AdminKey != ""
}

type cpampAnalyticsReq struct {
	FromMS   int64                  `json:"from_ms"`
	ToMS     int64                  `json:"to_ms"`
	NowMS    int64                  `json:"now_ms"`
	TimeZone string                 `json:"time_zone"`
	Filters  map[string]any         `json:"filters"`
	Include  map[string]any         `json:"include"`
}

type CPAMPSummary struct {
	TotalCalls   int64 `json:"total_calls"`
	SuccessCalls int64 `json:"success_calls"`
	FailureCalls int64 `json:"failure_calls"`
	TotalTokens  int64 `json:"total_tokens"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// FetchXAISummary returns xAI provider summary for [from,to].
func (c *CPAMPClient) FetchXAISummary(ctx context.Context, fromMS, toMS int64) (CPAMPSummary, error) {
	var zero CPAMPSummary
	if !c.Enabled() {
		return zero, fmt.Errorf("cpamp not configured")
	}
	if toMS <= fromMS {
		return zero, fmt.Errorf("invalid range")
	}
	body := cpampAnalyticsReq{
		FromMS:   fromMS,
		ToMS:     toMS,
		NowMS:    time.Now().UnixMilli(),
		TimeZone: "Asia/Shanghai",
		Filters:  map[string]any{"providers": []string{"xai"}},
		Include:  map[string]any{"summary": true},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v0/management/monitoring/analytics", bytes.NewReader(raw))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.AdminKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return zero, fmt.Errorf("cpamp status %d: %s", resp.StatusCode, truncate(string(b), 200))
	}
	var parsed struct {
		Summary CPAMPSummary `json:"summary"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return zero, err
	}
	return parsed.Summary, nil
}

// DayRangeShanghai returns [start,end) ms for calendar day of t in Asia/Shanghai.
func DayRangeShanghai(t time.Time) (fromMS, toMS int64) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	tt := t.In(loc)
	start := time.Date(tt.Year(), tt.Month(), tt.Day(), 0, 0, 0, 0, loc)
	end := start.Add(24 * time.Hour)
	return start.UnixMilli(), end.UnixMilli()
}

// PostWebhook sends a small JSON event; failures are non-fatal for callers.
func PostWebhook(ctx context.Context, url string, payload any) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}