package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"xai-autoban/cpasdk/pluginapi"
)

func TestClassifyFailure(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		status int
		want   time.Duration
	}{
		{http.StatusUnauthorized, banUnauthorized},
		{http.StatusPaymentRequired, banPayment},
		{http.StatusForbidden, banUnauthorized},
		{http.StatusTooManyRequests, banRateFallback},
	}
	for _, tt := range tests {
		entry, ok := classifyFailure(tt.status, nil, now)
		if !ok || entry.ResetAt.Sub(now) != tt.want {
			t.Fatalf("status %d: got %#v, ok=%v", tt.status, entry, ok)
		}
	}
	if _, ok := classifyFailure(http.StatusInternalServerError, nil, now); ok {
		t.Fatal("500 must not be banned")
	}
}

func TestPublicStatusPageHasNoManagementAuthentication(t *testing.T) {
	page := statusPage()
	for _, forbidden := range []string{"Authorization: Bearer", "Management key", "/v0/management/plugins/xai-autoban"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("page still contains authenticated management flow: %q", forbidden)
		}
	}
	for _, required := range []string{"/v0/resource/plugins/xai-autoban", "unbanSelected", "unbanStatus", "autoRefresh"} {
		if !strings.Contains(page, required) {
			t.Fatalf("page is missing %q", required)
		}
	}
}

func TestPublicUnbanByStatus(t *testing.T) {
	bans.clearAll()
	now := time.Now()
	bans.set("payment", banEntry{StatusCode: 402, ResetAt: now.Add(time.Hour)})
	bans.set("forbidden", banEntry{StatusCode: 403, ResetAt: now.Add(time.Hour)})
	response := publicAction(pluginapi.ManagementRequest{Query: url.Values{"op": {"unban-status"}, "status": {"402"}}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.StatusCode)
	}
	status := currentStatus()
	if status.Count != 1 || status.Bans[0].StatusCode != 403 {
		t.Fatalf("unexpected bans after action: %#v", status)
	}
}

func TestImportSnapshot(t *testing.T) {
	bans.clearAll()
	now := time.Now()
	snapshot := statusInfo{Bans: []banInfo{{AuthID: "restored", StatusCode: 429, Reason: "rate_limited", BannedAt: now.Format(time.RFC3339), ResetAt: now.Add(time.Hour).Format(time.RFC3339)}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	response := importSnapshot(raw)
	if response.StatusCode != http.StatusOK || currentStatus().Count != 1 {
		t.Fatalf("snapshot was not restored: response=%d status=%#v", response.StatusCode, currentStatus())
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	headers := http.Header{"Retry-After": {"90"}}
	entry, ok := classifyFailure(http.StatusTooManyRequests, headers, now)
	if !ok || entry.ResetAt.Sub(now) != 90*time.Second {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestSchedulerFiltersBannedXAI(t *testing.T) {
	bans.clearAll()
	now := time.Now()
	bans.set("bad", banEntry{StatusCode: 402, ResetAt: now.Add(time.Hour)})
	req := pluginapi.SchedulerPickRequest{Candidates: []pluginapi.SchedulerAuthCandidate{
		{ID: "bad", Provider: "xai", Priority: 100},
		{ID: "good", Provider: "xai", Priority: 10},
	}}
	raw, _ := jsonMarshal(req)
	responseRaw, err := handleSchedulerPick(raw)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	if err := jsonUnmarshal(responseRaw, &response); err != nil {
		t.Fatal(err)
	}
	var picked pluginapi.SchedulerPickResponse
	if err := jsonUnmarshal(response.Result, &picked); err != nil {
		t.Fatal(err)
	}
	if !picked.Handled || picked.AuthID != "good" {
		t.Fatalf("unexpected pick: %#v", picked)
	}
}

var jsonMarshal = func(v any) ([]byte, error) { return json.Marshal(v) }
var jsonUnmarshal = func(data []byte, v any) error { return json.Unmarshal(data, v) }
