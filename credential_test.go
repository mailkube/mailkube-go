package mailkube_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	mailkube "github.com/mailkube/mailkube-go"
)

const monthlyQuota = `{"name":"emails_per_month","limit":50000,"used":9310,"remaining":40690,"period":"2026-10"}`

// apiKeyBody returns the calling key's description with the given plan JSON spliced in.
func apiKeyBody(plan string) string {
	return `{"object":"api_key","key_id":"k3v9x2m8q1w7r4t6y0p5s2a8","name":"production-backend",` +
		`"description":null,"created_at":"2026-09-02T08:14:03Z","last_auth_at":"2026-10-19T16:41:22Z",` +
		`"allowed_source_ips":["203.0.113.0/24"],"domain":{"name":"example.com"},` +
		`"organization":{"name":"Example Ltd","alias":"example"}` + plan + `}`
}

const fullPlan = `,"plan":{"code":"00000000-0000-0000-0000-000000000003","name":"Momentum","status":"active",` +
	`"quotas":[{"name":"emails_per_day","limit":5000,"used":120,"remaining":4880,"period":"2026-10-19"},` +
	monthlyQuota + `]}`

func TestCredentialInfoGetsTheCallingKeyWithNoBody(t *testing.T) {
	client, seen := stubClient(t, 200, apiKeyBody(fullPlan), nil)

	if _, err := client.Credential.Info(context.Background()); err != nil {
		t.Fatalf("Info: %v", err)
	}
	if seen.req.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", seen.req.Method)
	}
	if path := seen.req.URL.EscapedPath(); path != "/mta/v1/api-keys/me" {
		t.Errorf("path = %s, want /mta/v1/api-keys/me", path)
	}
	if seen.body != nil {
		t.Errorf("body = %v, want none", seen.body)
	}
	if key := seen.req.Header.Get("Idempotency-Key"); key != "" {
		t.Errorf("Idempotency-Key = %q, want none", key)
	}
}

func TestCredentialInfoDecodesTheKeyAndItsPlan(t *testing.T) {
	client, _ := stubClient(t, 200, apiKeyBody(fullPlan), nil)

	key, err := client.Credential.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	want := mailkube.APIKey{
		Object:           "api_key",
		KeyID:            "k3v9x2m8q1w7r4t6y0p5s2a8",
		Name:             "production-backend",
		CreatedAt:        "2026-09-02T08:14:03Z",
		LastAuthAt:       "2026-10-19T16:41:22Z",
		AllowedSourceIPs: []string{"203.0.113.0/24"},
		Domain:           mailkube.APIKeyDomain{Name: "example.com"},
		Organization:     mailkube.APIKeyOrganization{Name: "Example Ltd", Alias: "example"},
		Plan: &mailkube.APIKeyPlan{
			Code:   "00000000-0000-0000-0000-000000000003",
			Name:   "Momentum",
			Status: "active",
			Quotas: []mailkube.APIKeyQuota{
				{Name: "emails_per_day", Limit: 5000, Used: 120, Remaining: 4880, Period: "2026-10-19"},
				{Name: "emails_per_month", Limit: 50000, Used: 9310, Remaining: 40690, Period: "2026-10"},
			},
		},
	}
	if !reflect.DeepEqual(*key, want) {
		t.Errorf("key = %+v\nwant  %+v", *key, want)
	}
}

func TestCredentialInfoReadsANullOrAbsentPlanAsNoPlan(t *testing.T) {
	for name, plan := range map[string]string{"null": `,"plan":null`, "absent": ``} {
		t.Run(name, func(t *testing.T) {
			client, _ := stubClient(t, 200, apiKeyBody(plan), nil)

			key, err := client.Credential.Info(context.Background())
			if err != nil {
				t.Fatalf("Info: %v", err)
			}
			if key.Plan != nil {
				t.Errorf("Plan = %+v, want nil", key.Plan)
			}
		})
	}
}

func TestCredentialInfoKeepsAMonthlyOnlyPlanMonthlyOnly(t *testing.T) {
	plan := `,"plan":{"code":"c","name":"Free","status":"active","quotas":[` + monthlyQuota + `]}`
	client, _ := stubClient(t, 200, apiKeyBody(plan), nil)

	key, err := client.Credential.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if len(key.Plan.Quotas) != 1 || key.Plan.Quotas[0].Name != "emails_per_month" {
		t.Errorf("Quotas = %+v, want emails_per_month only", key.Plan.Quotas)
	}
}

func TestCredentialInfoMapsARefusedKeyToAuthentication(t *testing.T) {
	body := `{"name":"invalid_api_key","message":"API key is invalid"}`
	client, _ := stubClient(t, 403, body, nil)

	_, err := client.Credential.Info(context.Background())
	if !errors.Is(err, mailkube.ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorName != mailkube.ErrorNameInvalidAPIKey {
		t.Errorf("err = %v, want an APIError named invalid_api_key", err)
	}
}

func TestCredentialInfoCarriesRetryAfterOnARateLimit(t *testing.T) {
	body := `{"name":"rate_limit_exceeded","message":"slow down"}`
	client, _ := stubClient(t, 429, body, map[string]string{"Retry-After": "59"})

	_, err := client.Credential.Info(context.Background())
	var apiErr *mailkube.APIError
	if !errors.Is(err, mailkube.ErrRateLimit) || !errors.As(err, &apiErr) || apiErr.RetryAfter != 59 {
		t.Errorf("err = %v, want ErrRateLimit with RetryAfter 59", err)
	}
}

func TestCredentialInfoRefusesANonObjectSuccessBody(t *testing.T) {
	client, _ := stubClient(t, 200, `[]`, nil)

	_, err := client.Credential.Info(context.Background())
	if !errors.Is(err, mailkube.ErrUnexpectedResponse) || errors.Is(err, mailkube.ErrAuthentication) {
		t.Errorf("err = %v, want ErrUnexpectedResponse", err)
	}
	if err != nil && !strings.Contains(err.Error(), "JSON object") {
		t.Errorf("err = %v, want the JSON-object explanation", err)
	}
}
