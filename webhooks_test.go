package mailkube_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	mailkube "github.com/mailkube/mailkube-go"
)

const (
	secret = "whsec_test"
	body   = `{"type":"email.sent","data":{"id":"abc123"}}`
)

func signedHeaders(at time.Time) http.Header {
	return signedHeadersFor(body, at)
}

// signedHeadersFor signs an arbitrary payload, so the handler tests can post something other
// than the package-level body without repeating the signing scheme.
func signedHeadersFor(payload string, at time.Time) http.Header {
	timestamp := at.Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("wh_1." + timestamp + "."))
	mac.Write([]byte(payload))

	headers := http.Header{}
	headers.Set("X-Webhook-Id", "wh_1")
	headers.Set("X-Webhook-Ts", timestamp)
	headers.Set("X-Webhook-Sig", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return headers
}

func TestAHeaderFuncAdaptsANonStandardHeaderSource(t *testing.T) {
	// The shape a fasthttp or Fiber receiver needs: Fiber's Ctx.Get is variadic, so it cannot
	// be converted directly and has to be wrapped like this.
	headers := signedHeaders(time.Now())
	lookup := mailkube.HeaderFunc(func(name string) string { return headers.Get(name) })

	if _, err := mailkube.VerifySignature([]byte(body), lookup, secret, 0); err != nil {
		t.Fatalf("VerifySignature through a HeaderFunc: %v", err)
	}
}

func TestVerifyReturnsATypedEventCarryingTheDeliveryHeaders(t *testing.T) {
	event, err := mailkube.Verify([]byte(body), signedHeaders(time.Now()), secret, 0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if event.Type != mailkube.EventTypeEmailSent {
		t.Errorf("type = %q", event.Type)
	}
	// The dedup key: stable across every retry of this delivery.
	if event.ID != "wh_1" {
		t.Errorf("ID = %q, want the X-Webhook-Id value", event.ID)
	}
	if event.Timestamp == "" {
		t.Error("Timestamp must carry the X-Webhook-Ts value")
	}
}

func TestVerifyRefusesToParseAnUnverifiedBody(t *testing.T) {
	_, err := mailkube.Verify([]byte(body), signedHeaders(time.Now()), "whsec_wrong", 0)
	if !errors.Is(err, mailkube.ErrSignatureVerification) {
		t.Errorf("err = %v, want ErrSignatureVerification", err)
	}
}

func TestAValidSignatureReturnsTheRawBody(t *testing.T) {
	verified, err := mailkube.VerifySignature([]byte(body), signedHeaders(time.Now()), secret, 0)
	if err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if string(verified) != body {
		t.Errorf("verified = %q", verified)
	}
}

func TestThePrefixIsOptional(t *testing.T) {
	headers := signedHeaders(time.Now())
	headers.Set("X-Webhook-Sig", headers.Get("X-Webhook-Sig")[len("sha256="):])

	if _, err := mailkube.VerifySignature([]byte(body), headers, secret, 0); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestATamperedBodyFails(t *testing.T) {
	_, err := mailkube.VerifySignature([]byte(`{"type":"email.bounced"}`), signedHeaders(time.Now()), secret, 0)
	if !errors.Is(err, mailkube.ErrSignatureVerification) {
		t.Errorf("err = %v", err)
	}
}

func TestAWrongSecretFails(t *testing.T) {
	_, err := mailkube.VerifySignature([]byte(body), signedHeaders(time.Now()), "whsec_other", 0)
	if !errors.Is(err, mailkube.ErrSignatureVerification) {
		t.Errorf("err = %v", err)
	}
}

func TestAMissingHeaderFails(t *testing.T) {
	for _, name := range []string{"X-Webhook-Id", "X-Webhook-Ts", "X-Webhook-Sig"} {
		headers := signedHeaders(time.Now())
		headers.Del(name)

		if _, err := mailkube.VerifySignature([]byte(body), headers, secret, 0); !errors.Is(err, mailkube.ErrSignatureVerification) {
			t.Errorf("missing %s: err = %v", name, err)
		}
	}
}

func TestAStaleTimestampFails(t *testing.T) {
	_, err := mailkube.VerifySignature([]byte(body), signedHeaders(time.Now().Add(-time.Hour)), secret, 0)
	if !errors.Is(err, mailkube.ErrSignatureVerification) {
		t.Errorf("err = %v", err)
	}
}

func TestAMalformedTimestampFails(t *testing.T) {
	headers := signedHeaders(time.Now())
	headers.Set("X-Webhook-Ts", "not-a-date")

	if _, err := mailkube.VerifySignature([]byte(body), headers, secret, 0); !errors.Is(err, mailkube.ErrSignatureVerification) {
		t.Errorf("err = %v", err)
	}
}

func TestACustomToleranceIsHonoured(t *testing.T) {
	headers := signedHeaders(time.Now().Add(-10 * time.Minute))

	if _, err := mailkube.VerifySignature([]byte(body), headers, secret, time.Hour); err != nil {
		t.Errorf("a one-hour tolerance should accept a ten-minute-old webhook: %v", err)
	}
}
