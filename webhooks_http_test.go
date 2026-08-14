package mailkube_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mailkube "github.com/mailkube/mailkube-go"
)

// serve runs one request through a handler and returns the recorded response.
func serve(handler mailkube.WebhookHandler, req *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

// delivery builds a signed POST carrying payload.
func delivery(payload string, at time.Time) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(payload))
	req.Header = signedHeadersFor(payload, at)
	return req
}

func TestTheHandshakeEchoesTheChallenge(t *testing.T) {
	// Without this the platform refuses to save the endpoint and no event is ever delivered.
	req := httptest.NewRequest(http.MethodGet, "/webhooks?hub.mode=subscribe&hub.challenge=deadbeef", nil)

	resp := serve(mailkube.WebhookHandler{Secret: secret}, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if got := strings.TrimSpace(resp.Body.String()); got != "deadbeef" {
		t.Errorf("body = %q, want the challenge echoed verbatim", got)
	}
	if got := resp.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content-type = %q", got)
	}
}

func TestTheHandshakeIsAnsweredBeforeAnySignatureCheck(t *testing.T) {
	// The probe carries no X-Webhook-* headers at all, so a handler that verified first would
	// answer 401 and the endpoint would never be created.
	req := httptest.NewRequest(http.MethodGet, "/webhooks?hub.mode=subscribe&hub.challenge=abc", nil)

	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for an unsigned handshake", resp.Code)
	}
}

func TestAGetThatIsNotAHandshakeIsRejected(t *testing.T) {
	cases := map[string]string{
		"no mode":          "/webhooks?hub.challenge=abc",
		"wrong mode":       "/webhooks?hub.mode=unsubscribe&hub.challenge=abc",
		"no challenge":     "/webhooks?hub.mode=subscribe",
		"absurd challenge": "/webhooks?hub.mode=subscribe&hub.challenge=" + strings.Repeat("a", 300),
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			resp := serve(mailkube.WebhookHandler{Secret: secret}, httptest.NewRequest(http.MethodGet, target, nil))
			if resp.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.Code)
			}
			if strings.Contains(resp.Body.String(), "aaaa") {
				t.Error("an over-long challenge must not be reflected")
			}
		})
	}
}

func TestADeliveryIsVerifiedParsedAndAcknowledged(t *testing.T) {
	var seen *mailkube.Event
	handler := mailkube.WebhookHandler{
		Secret: secret,
		OnEvent: func(_ context.Context, event *mailkube.Event) error {
			seen = event
			return nil
		},
	}

	resp := serve(handler, delivery(body, time.Now()))

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.Code)
	}
	if seen == nil {
		t.Fatal("OnEvent was never called")
	}
	if seen.Type != mailkube.EventTypeEmailSent || seen.ID != "wh_1" {
		t.Errorf("event = %+v", seen)
	}
}

func TestABadSignatureIsUnauthorized(t *testing.T) {
	req := delivery(body, time.Now())
	req.Header.Set("X-Webhook-Sig", "sha256=00")

	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.Code)
	}
}

func TestAStaleDeliveryIsUnauthorized(t *testing.T) {
	req := delivery(body, time.Now().Add(-1*time.Hour))

	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.Code)
	}
}

func TestACustomToleranceIsHonouredByTheHandler(t *testing.T) {
	req := delivery(body, time.Now().Add(-30*time.Minute))
	handler := mailkube.WebhookHandler{Secret: secret, Tolerance: time.Hour}

	if resp := serve(handler, req); resp.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 inside the widened window", resp.Code)
	}
}

func TestASignedBodyThatIsNotAnEventIsABadRequest(t *testing.T) {
	req := delivery("not json", time.Now())

	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.Code)
	}
}

func TestAnOversizedDeliveryIsRejectedWithoutReadingItAll(t *testing.T) {
	huge := `{"type":"email.sent","data":{"note":"` + strings.Repeat("x", 4096) + `"}}`
	handler := mailkube.WebhookHandler{Secret: secret, MaxBodyBytes: 64}

	if resp := serve(handler, delivery(huge, time.Now())); resp.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.Code)
	}
}

func TestAHandlerErrorIsAServerError(t *testing.T) {
	handler := mailkube.WebhookHandler{
		Secret:  secret,
		OnEvent: func(context.Context, *mailkube.Event) error { return errors.New("queue is down") },
	}

	if resp := serve(handler, delivery(body, time.Now())); resp.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.Code)
	}
}

func TestANilOnEventStillVerifiesAndAcknowledges(t *testing.T) {
	if resp := serve(mailkube.WebhookHandler{Secret: secret}, delivery(body, time.Now())); resp.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.Code)
	}

	req := delivery(body, time.Now())
	req.Header.Set("X-Webhook-Sig", "sha256=00")
	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusUnauthorized {
		t.Error("a nil OnEvent must still reject an unverified delivery")
	}
}

func TestOnErrorOwnsTheErrorResponse(t *testing.T) {
	var seenErr error
	handler := mailkube.WebhookHandler{
		Secret: secret,
		OnError: func(w http.ResponseWriter, _ *http.Request, err error) {
			seenErr = err
			w.WriteHeader(http.StatusTeapot)
		},
	}

	req := delivery(body, time.Now())
	req.Header.Set("X-Webhook-Sig", "sha256=00")

	if resp := serve(handler, req); resp.Code != http.StatusTeapot {
		t.Errorf("status = %d, want the OnError status", resp.Code)
	}
	if !errors.Is(seenErr, mailkube.ErrSignatureVerification) {
		t.Errorf("OnError saw %v", seenErr)
	}
}

func TestAnUnsupportedMethodIsRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/webhooks", strings.NewReader(body))

	if resp := serve(mailkube.WebhookHandler{Secret: secret}, req); resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestTheHandlerMountsAsAnHTTPHandlerByValueAndByPointer(t *testing.T) {
	handler := mailkube.WebhookHandler{Secret: secret}

	// Both forms must satisfy http.Handler, since a caller may write either.
	var _ http.Handler = handler
	var _ http.Handler = &handler

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "?hub.mode=subscribe&hub.challenge=live")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if string(raw) != "live" {
		t.Errorf("body = %q", raw)
	}
}
