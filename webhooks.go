package mailkube

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	signaturePrefix  = "sha256="
	defaultTolerance = 300 * time.Second

	headerWebhookID        = "X-Webhook-Id"
	headerWebhookTimestamp = "X-Webhook-Ts"
	headerWebhookSignature = "X-Webhook-Sig"
)

// HeaderGetter is the one thing webhook verification needs from a request: a header lookup.
//
// http.Header satisfies it as it stands, so net/http, chi, gin and echo hand over r.Header
// unchanged. A framework that keeps headers elsewhere wraps its own accessor in HeaderFunc.
// An implementation must look names up case-insensitively.
type HeaderGetter interface {
	Get(name string) string
}

// HeaderFunc adapts a plain lookup function to HeaderGetter.
//
// Fiber's Ctx.Get is variadic, so it needs the wrapper form rather than a bare conversion:
//
//	mailkube.HeaderFunc(func(name string) string { return c.Get(name) })
type HeaderFunc func(name string) string

// Get implements HeaderGetter.
func (f HeaderFunc) Get(name string) string { return f(name) }

// VerifySignature verifies a webhook's signature and timestamp freshness over the raw body.
//
// Verification is pure and dependency-free: no HTTP client, no configuration, so you call it
// directly inside your webhook handler. Verify against the raw received bytes; never decode
// then re-encode, or the signature will not match.
//
// The signed input is "{id}.{timestamp}." followed by the raw body, HMAC-SHA256 keyed by the
// endpoint's signing secret, hex-encoded, and sent as X-Webhook-Sig: sha256=<hex>. X-Webhook-Ts
// is an ISO-8601 timestamp checked for freshness; X-Webhook-Id is stable across retries, so use
// it to deduplicate. A tolerance of zero means the default 300 seconds.
//
// Errors wrap ErrSignatureVerification.
func VerifySignature(payload []byte, headers HeaderGetter, secret string, tolerance time.Duration) ([]byte, error) {
	if tolerance <= 0 {
		tolerance = defaultTolerance
	}

	id := headers.Get(headerWebhookID)
	timestamp := headers.Get(headerWebhookTimestamp)
	signature := headers.Get(headerWebhookSignature)
	if id == "" || timestamp == "" || signature == "" {
		return nil, fmt.Errorf("%w: missing required webhook signature headers", ErrSignatureVerification)
	}

	if err := checkFreshness(timestamp, tolerance); err != nil {
		return nil, err
	}

	expected := signatureMAC(id, timestamp, payload, secret)

	provided, err := hex.DecodeString(strings.TrimPrefix(signature, signaturePrefix))
	if err != nil || !hmac.Equal(provided, expected) {
		return nil, fmt.Errorf("%w: signature mismatch", ErrSignatureVerification)
	}
	return payload, nil
}

// Sign returns the X-Webhook-Sig header value for a payload, as the platform would send it.
//
// This is the counterpart to VerifySignature, and it exists so that anything producing a
// webhook delivery — a test fixture, a local replay tool, a fake endpoint in your own test
// suite — computes the signature the same way this package verifies it, instead of
// reimplementing the construction and drifting from it.
//
// The id and timestamp must be the values sent as X-Webhook-Id and X-Webhook-Ts, since both
// are part of the signed input. The result carries the "sha256=" prefix and is ready to use
// as the header value:
//
//	sig := mailkube.Sign(id, timestamp, body, secret)
//	req.Header.Set("X-Webhook-Sig", sig)
//
// Freshness is not this function's concern: it signs the timestamp it is given, so a caller
// replaying an old capture can reproduce the original signature exactly.
func Sign(id, timestamp string, payload []byte, secret string) string {
	return signaturePrefix + hex.EncodeToString(signatureMAC(id, timestamp, payload, secret))
}

// signatureMAC computes the raw HMAC-SHA256 over the signed input the contract defines:
// "{id}.{timestamp}." followed by the raw body. One implementation, so signing and verifying
// cannot disagree.
func signatureMAC(id, timestamp string, payload []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(payload)
	return mac.Sum(nil)
}

// Verify verifies a webhook and returns the parsed event.
//
// The convenience combinator over VerifySignature and ParseEvent, and the one that fills in
// Event.ID and Event.Timestamp from the delivery headers. Deduplicate on Event.ID: the platform
// retries a delivery up to 15 times and the id is stable across every attempt.
func Verify(payload []byte, headers HeaderGetter, secret string, tolerance time.Duration) (*Event, error) {
	verified, err := VerifySignature(payload, headers, secret, tolerance)
	if err != nil {
		return nil, err
	}

	event, err := ParseEvent(verified)
	if err != nil {
		return nil, err
	}
	event.ID = headers.Get(headerWebhookID)
	event.Timestamp = headers.Get(headerWebhookTimestamp)
	return event, nil
}

// checkFreshness reports an error if the ISO-8601 timestamp is outside the tolerance window.
func checkFreshness(timestamp string, tolerance time.Duration) error {
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return fmt.Errorf("%w: malformed %s timestamp", ErrSignatureVerification, headerWebhookTimestamp)
	}
	if math.Abs(time.Since(parsed).Seconds()) > tolerance.Seconds() {
		return fmt.Errorf("%w: timestamp is outside the freshness window", ErrSignatureVerification)
	}
	return nil
}
