package mailkube_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	mailkube "github.com/mailkube/mailkube-go"
)

// roundTripFunc adapts a function to http.RoundTripper so tests can stub transport without
// touching the network. This is what the WithHTTPClient seam exists for.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type capture struct {
	req  *http.Request
	body map[string]any
}

func stubClient(t *testing.T, status int, payload string, headers map[string]string) (*mailkube.Client, *capture) {
	t.Helper()
	seen := &capture{}
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen.req = r
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &seen.body)
		}
		resp := &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(payload)),
			Header:     http.Header{},
		}
		for name, value := range headers {
			resp.Header.Set(name, value)
		}
		return resp, nil
	})}

	client, err := mailkube.New(mailkube.WithAPIKey("mk_test"), mailkube.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, seen
}

func minimal() mailkube.SendEmailParams {
	return mailkube.SendEmailParams{From: "a@x.com", To: []string{"b@y.com"}, Subject: "Hi"}
}

func TestSendPostsToTheEmailsEndpoint(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)

	params := minimal()
	params.HTML = "<p>Hi</p>"
	if _, err := client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if seen.req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", seen.req.Method)
	}
	if got, want := seen.req.URL.String(), mailkube.DefaultBaseURL+"emails"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if seen.body["from"] != "a@x.com" || seen.body["subject"] != "Hi" || seen.body["html"] != "<p>Hi</p>" {
		t.Errorf("body = %v", seen.body)
	}
}

func TestEveryRequestCarriesBearerAuthAndTheVersionedUserAgent(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)
	if _, err := client.Emails.Send(context.Background(), minimal()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := seen.req.Header.Get("Authorization"); got != "Bearer mk_test" {
		t.Errorf("Authorization = %q", got)
	}
	if got := seen.req.Header.Get("User-Agent"); !strings.HasPrefix(got, "mailkube-go/") {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestUnsetFieldsAreOmittedRatherThanSentAsNull(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)
	if _, err := client.Emails.Send(context.Background(), minimal()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for _, key := range []string{"html", "text", "cc", "bcc", "reply_to", "attachments", "tags"} {
		if _, present := seen.body[key]; present {
			t.Errorf("body unexpectedly contains %q", key)
		}
	}
}

func TestAttachmentsAndTagsAreRenderedForTheWire(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)

	params := minimal()
	params.Attachments = []mailkube.Attachment{
		{Filename: "a.txt", Content: []byte("hello"), ContentType: "text/plain"},
	}
	params.Tags = []mailkube.Tag{
		{Name: "campaign", Value: "spring"},
	}
	if _, err := client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	attachments, _ := seen.body["attachments"].([]any)
	first, _ := attachments[0].(map[string]any)
	if first["content"] != "aGVsbG8=" || first["filename"] != "a.txt" {
		t.Errorf("attachment = %v", first)
	}
	tags, _ := seen.body["tags"].([]any)
	tag, _ := tags[0].(map[string]any)
	if tag["name"] != "campaign" || tag["value"] != "spring" {
		t.Errorf("tag = %v", tag)
	}
}

func TestTheIdempotencyKeyTravelsAsAHeader(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)

	params := minimal()
	params.IdempotencyKey = "key-1"
	if _, err := client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := seen.req.Header.Get("Idempotency-Key"); got != "key-1" {
		t.Errorf("Idempotency-Key = %q", got)
	}
	if _, present := seen.body["idempotency_key"]; present {
		t.Error("idempotency_key leaked into the body")
	}
}

func TestScheduledAtIsRenderedAsRFC3339(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)

	params := minimal()
	params.ScheduledAt = time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC)
	if _, err := client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if seen.body["scheduled_at"] != "2026-08-20T07:00:00Z" {
		t.Errorf("scheduled_at = %v", seen.body["scheduled_at"])
	}
}

func TestSendReturnsTheParsedEmail(t *testing.T) {
	client, _ := stubClient(t, 200, `{"id":"abc123","message_id":"<abc123@msg>"}`, nil)

	email, err := client.Emails.Send(context.Background(), minimal())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if email.ID != "abc123" || email.MessageID != "<abc123@msg>" || email.IsScheduled() {
		t.Errorf("email = %+v", email)
	}
}

func TestAReplayedResponseIsReportedFromTheHeader(t *testing.T) {
	client, _ := stubClient(t, 200, `{"id":"abc123"}`, map[string]string{"Idempotent-Replayed": "true"})

	email, err := client.Emails.Send(context.Background(), minimal())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !email.IdempotentReplayed {
		t.Error("IdempotentReplayed = false, want true")
	}
}

func TestAScheduledAckWidensTheSameModel(t *testing.T) {
	client, _ := stubClient(t, 202, `{"id":"abc123","status":"scheduled","scheduled_at":"2026-08-20T07:00:00Z"}`, nil)

	email, err := client.Emails.Send(context.Background(), minimal())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !email.IsScheduled() || email.Status != "scheduled" {
		t.Errorf("email = %+v", email)
	}
}

func TestStatusChoosesTheErrorCategory(t *testing.T) {
	cases := []struct {
		status   int
		sentinel error
	}{
		{400, mailkube.ErrBadRequest},
		{403, mailkube.ErrAuthentication},
		{404, mailkube.ErrNotFound},
		{409, mailkube.ErrConflict},
		{422, mailkube.ErrInvalidRequest},
		{429, mailkube.ErrRateLimit},
		{500, mailkube.ErrServer},
		{503, mailkube.ErrServer},
		{418, mailkube.ErrAPI},
	}
	for _, tc := range cases {
		client, _ := stubClient(t, tc.status, `{"name":"validation_error","message":"nope"}`, nil)
		_, err := client.Emails.Send(context.Background(), minimal())
		if !errors.Is(err, tc.sentinel) {
			t.Errorf("status %d: err = %v, want %v", tc.status, err, tc.sentinel)
		}
		if !errors.Is(err, mailkube.ErrAPI) {
			t.Errorf("status %d: every API error should also match ErrAPI", tc.status)
		}
	}
}

func TestRetryAfterAndRequestIDAreReadOffTheHeaders(t *testing.T) {
	client, _ := stubClient(t, 429, `{"name":"rate_limit_exceeded","message":"slow down"}`,
		map[string]string{"Retry-After": "30", "X-Request-Id": "req_42"})

	_, err := client.Emails.Send(context.Background(), minimal())
	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an *APIError", err)
	}
	if apiErr.RetryAfter != 30 || apiErr.RequestID != "req_42" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if apiErr.ErrorName != mailkube.ErrorNameRateLimitExceeded {
		t.Errorf("ErrorName = %q", apiErr.ErrorName)
	}
}

// TestTheRequestIDIsReadWhateverCasingTheServerSent pins the case-insensitive header lookup.
//
// HTTP header names are case-insensitive and the gateway is free to send `x-request-id`. The
// response is parsed off a raw wire buffer rather than assembled with http.Header.Set, because
// Set canonicalizes the name and would hide a lookup that only matched one spelling.
func TestTheRequestIDIsReadWhateverCasingTheServerSent(t *testing.T) {
	const wire = "HTTP/1.1 403 Forbidden\r\n" +
		"Content-Type: application/json\r\n" +
		"x-request-id: req_lowercase\r\n" +
		"\r\n" +
		`{"name":"invalid_api_key","message":"nope"}`

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return http.ReadResponse(bufio.NewReader(strings.NewReader(wire)), r)
	})}
	client, err := mailkube.New(mailkube.WithAPIKey("mk_test"), mailkube.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Emails.Send(context.Background(), minimal())
	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an *APIError", err)
	}
	if apiErr.RequestID != "req_lowercase" {
		t.Errorf("RequestID = %q, want the id read off the lowercased header", apiErr.RequestID)
	}
}

func TestAnUnknownErrorNameIsReportedVerbatim(t *testing.T) {
	client, _ := stubClient(t, 400, `{"name":"invented_next_year","message":"hi"}`, nil)

	_, err := client.Emails.Send(context.Background(), minimal())
	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorName != "invented_next_year" {
		t.Errorf("err = %v", err)
	}
}

func TestAnUndecodableErrorBodyStillMapsByStatus(t *testing.T) {
	client, _ := stubClient(t, 500, `<html>oops</html>`, nil)

	_, err := client.Emails.Send(context.Background(), minimal())
	if !errors.Is(err, mailkube.ErrServer) {
		t.Errorf("err = %v, want ErrServer", err)
	}
	var apiErr *mailkube.APIError
	if errors.As(err, &apiErr) && apiErr.Error() != "HTTP 500" {
		t.Errorf("Error() = %q, want the status fallback", apiErr.Error())
	}
}

func TestATransportFailureIsAConnectionError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})}
	client, err := mailkube.New(mailkube.WithAPIKey("mk_test"), mailkube.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = client.Emails.Send(context.Background(), minimal())
	if !errors.Is(err, mailkube.ErrConnection) {
		t.Errorf("err = %v, want ErrConnection", err)
	}
	if errors.Is(err, mailkube.ErrAPI) {
		t.Error("a transport failure must not be an API error")
	}
}

func TestASuccessBodyWithoutAnIDIsAnError(t *testing.T) {
	client, _ := stubClient(t, 200, `{"unexpected":true}`, nil)

	_, err := client.Emails.Send(context.Background(), minimal())
	if !errors.Is(err, mailkube.ErrUnexpectedResponse) {
		t.Errorf("err = %v, want ErrUnexpectedResponse", err)
	}
	// A 2xx the SDK could not read is deliberately not an API error: the server accepted the
	// request, so there is no error envelope to report.
	if errors.Is(err, mailkube.ErrAPI) {
		t.Error("a malformed success body must not be reported as an API error")
	}
	if !errors.Is(err, mailkube.ErrMailkube) {
		t.Error("every error from this package must match the base sentinel")
	}
}

func TestTheSendBodyCarriesExactlyTheExpectedKeys(t *testing.T) {
	// The invariant the map-to-struct rewrite has to preserve. Byte equality is not the right
	// guard (encoding/json sorts map keys but emits struct fields in declaration order), the
	// key set is: this catches both a field that starts appearing and one that stops.
	full := mailkube.SendEmailParams{
		From: "a@x.com", To: []string{"b@y.com"}, Subject: "Hi",
		HTML: "<p>hi</p>", Text: "hi", CC: []string{"c@y.com"}, BCC: []string{"d@y.com"},
		ReplyTo: []string{"r@x.com"}, Headers: map[string]string{"In-Reply-To": "<a@b>"},
		Attachments: []mailkube.Attachment{{Filename: "a.txt", Content: []byte("hi")}},
		Tags:        []mailkube.Tag{{Name: "campaign", Value: "spring"}},
		TemplateID:  "tpl_1", TemplateVersion: "latest", Variables: map[string]string{"name": "Ada"},
		Topic: "news", ScheduledAt: time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC), BatchID: "launch",
		IdempotencyKey: "key-1",
	}

	for name, tc := range map[string]struct {
		params mailkube.SendEmailParams
		want   []string
	}{
		"minimal": {minimal(), []string{"from", "to", "subject"}},
		"every field": {full, []string{
			"from", "to", "subject", "html", "text", "cc", "bcc", "reply_to", "headers",
			"attachments", "tags", "template_id", "template_version", "variables", "topic",
			"scheduled_at", "batch_id",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)
			if _, err := client.Emails.Send(context.Background(), tc.params); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := make([]string, 0, len(seen.body))
			for key := range seen.body {
				got = append(got, key)
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("body keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestABlankTagValueIsStillSent(t *testing.T) {
	// The server requires the key to be present and allows it blank, so `value` must never
	// pick up an omitempty.
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)
	params := minimal()
	params.Tags = []mailkube.Tag{{Name: "campaign"}}

	if _, err := client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	tags, ok := seen.body["tags"].([]any)
	if !ok || len(tags) != 1 {
		t.Fatalf("tags = %v", seen.body["tags"])
	}
	tag, _ := tags[0].(map[string]any)
	if _, present := tag["value"]; !present {
		t.Errorf("tag = %v, want an explicit empty value", tag)
	}
}

func TestContentTypeIsSentOnlyWithABody(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)
	if _, err := client.Emails.Send(context.Background(), minimal()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := seen.req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type on a send = %q", got)
	}

	client, seen = stubClient(t, 200, `{"id":"se_1","status":"canceled"}`, nil)
	if _, err := client.ScheduledEmails.Cancel(context.Background(), "se_1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := seen.req.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type on a body-less DELETE = %q, want none", got)
	}
	if seen.req.Body != nil && seen.req.ContentLength != 0 {
		t.Errorf("ContentLength = %d, want 0", seen.req.ContentLength)
	}
}

func TestTheClientRefusesARedirectRatherThanDowngradingTheMethod(t *testing.T) {
	// Go's default policy rewrites a redirected DELETE into a GET and drops the body, which on
	// this API answers 200 with the still-scheduled row: the caller would be told a cancel
	// succeeded that never happened.
	var followed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/elsewhere") {
			followed = true
			_, _ = w.Write([]byte(`{"id":"se_1","status":"scheduled"}`))
			return
		}
		http.Redirect(w, r, "/v1/elsewhere", http.StatusMovedPermanently)
	}))
	defer server.Close()

	client, err := mailkube.New(mailkube.WithAPIKey("mk_test"), mailkube.WithBaseURL(server.URL+"/v1/"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err = client.ScheduledEmails.Cancel(context.Background(), "se_1"); !errors.Is(err, mailkube.ErrConnection) {
		t.Errorf("err = %v, want ErrConnection", err)
	}
	if followed {
		t.Error("the client followed the redirect")
	}
}

func TestTheLoggerRecordsRequestsWithTheSecretsRedacted(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"id":"abc123"}`)),
			Header:     http.Header{"X-Request-Id": []string{"req_1"}},
		}, nil
	})}

	client, err := mailkube.New(mailkube.WithAPIKey("mk_secret_value"),
		mailkube.WithHTTPClient(httpClient), mailkube.WithLogger(logger))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	params := minimal()
	params.IdempotencyKey = "order-42"
	if _, err = client.Emails.Send(context.Background(), params); err != nil {
		t.Fatalf("Send: %v", err)
	}

	written := log.String()
	for _, want := range []string{"mailkube request", "mailkube response", "req_1", "***"} {
		if !strings.Contains(written, want) {
			t.Errorf("log is missing %q:\n%s", want, written)
		}
	}
	for _, secret := range []string{"mk_secret_value", "order-42"} {
		if strings.Contains(written, secret) {
			t.Errorf("log leaked %q:\n%s", secret, written)
		}
	}
}

func TestTheClientIsSilentUnlessAskedToLog(t *testing.T) {
	client, _ := stubClient(t, 200, `{"id":"abc123"}`, nil)

	// Nothing to assert on stdout from here, so this covers the default path: it must not
	// panic on a nil logger, which is what the discard handler exists to prevent.
	if _, err := client.Emails.Send(context.Background(), minimal()); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
