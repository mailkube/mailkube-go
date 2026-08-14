package mailkube

import (
	"errors"
	"fmt"
)

// ErrMailkube is the base every error this package produces matches.
//
// The contract requires one base error type so a caller can ask "did this come from the SDK?"
// without enumerating categories. In Go that is a sentinel every other sentinel wraps, rather
// than a class hierarchy:
//
//	if errors.Is(err, mailkube.ErrMailkube) { ... }
var ErrMailkube = errors.New("mailkube")

// Sentinel errors for the categories the contract defines. Compare with errors.Is:
//
//	if errors.Is(err, mailkube.ErrRateLimit) { ... }
//
// This is a deliberate deviation from the other SDKs, which expose one exception class per
// status. Go's convention is errors.Is over a sentinel rather than a type hierarchy, and a
// struct per status would neither read nor compose idiomatically. The categories, and the
// status that selects each, are identical to every other mailkube SDK.
var (
	// ErrConnection is a transport-level failure with no HTTP response.
	ErrConnection = fmt.Errorf("%w: connection failure", ErrMailkube)
	// ErrSignatureVerification means a webhook signature could not be verified.
	ErrSignatureVerification = fmt.Errorf("%w: signature verification failed", ErrMailkube)
	// ErrUnexpectedResponse is a 2xx response whose body is not the shape the verb expects.
	//
	// Deliberately not an API error: the request succeeded as far as the server is concerned,
	// so there is no error envelope to report, and treating it as one would tell a caller the
	// API rejected a request it actually accepted.
	ErrUnexpectedResponse = fmt.Errorf("%w: unexpected response", ErrMailkube)
	// ErrBadRequest is HTTP 400: the request envelope was invalid.
	ErrBadRequest = fmt.Errorf("%w: bad request", ErrMailkube)
	// ErrAuthentication is HTTP 403: authentication failed or is forbidden.
	ErrAuthentication = fmt.Errorf("%w: authentication failed", ErrMailkube)
	// ErrNotFound is HTTP 404: a referenced resource was not found.
	ErrNotFound = fmt.Errorf("%w: not found", ErrMailkube)
	// ErrConflict is HTTP 409: an idempotency conflict.
	ErrConflict = fmt.Errorf("%w: conflict", ErrMailkube)
	// ErrInvalidRequest is HTTP 422: the request was rejected by a send-policy check.
	ErrInvalidRequest = fmt.Errorf("%w: invalid request", ErrMailkube)
	// ErrRateLimit is HTTP 429: the rate limit was exceeded. Inspect APIError.RetryAfter.
	ErrRateLimit = fmt.Errorf("%w: rate limit exceeded", ErrMailkube)
	// ErrServer is HTTP 5xx: an unexpected server error. Safe to retry with backoff.
	ErrServer = fmt.Errorf("%w: server error", ErrMailkube)
	// ErrAPI is any other non-2xx response, and a supertype of every category above it.
	ErrAPI = fmt.Errorf("%w: api error", ErrMailkube)
)

// statusSentinels maps an HTTP status to its category. Any other 5xx is ErrServer; the rest ErrAPI.
var statusSentinels = map[int]error{
	400: ErrBadRequest,
	403: ErrAuthentication,
	404: ErrNotFound,
	409: ErrConflict,
	422: ErrInvalidRequest,
	429: ErrRateLimit,
}

// Error-name constants for the documented values of the API error envelope.
//
// These are constants for discoverability, not a closed set: APIError.ErrorName stays a plain
// string, so a name this release has never heard of is reported verbatim. The list tracks the
// public error reference and is kept sorted; add one when the reference gains a name.
const (
	ErrorNameApplicationError            = "application_error"
	ErrorNameBodyContentRejected         = "body_content_rejected"
	ErrorNameBrowserNotAllowed           = "browser_not_allowed"
	ErrorNameConcurrentIdempotentRequest = "concurrent_idempotent_requests"
	ErrorNameFromDomainNotAllowed        = "from_domain_not_allowed"
	ErrorNameInvalidAPIKey               = "invalid_api_key"
	ErrorNameInvalidAttachment           = "invalid_attachment"
	ErrorNameInvalidFromAddress          = "invalid_from_address"
	ErrorNameInvalidIdempotencyKey       = "invalid_idempotency_key"
	ErrorNameInvalidIdempotentRequest    = "invalid_idempotent_request"
	ErrorNameInvalidRequestBody          = "invalid_request_body"
	ErrorNameLinkReputationBlocked       = "link_reputation_blocked"
	ErrorNameMaxMessageSizeExceeded      = "max_message_size_exceeded"
	ErrorNameMaxRecipientsExceeded       = "max_recipients_exceeded"
	ErrorNameMethodNotAllowed            = "method_not_allowed"
	ErrorNameMissingRequiredField        = "missing_required_field"
	ErrorNameMissingRequiredVariable     = "missing_required_variable"
	ErrorNameMissingUserAgent            = "missing_user_agent"
	ErrorNameNotAcceptable               = "not_acceptable"
	ErrorNameQuotaExceeded               = "quota_exceeded"
	ErrorNameRateLimitExceeded           = "rate_limit_exceeded"
	ErrorNameScheduledEmailNotFound      = "scheduled_email_not_found"
	ErrorNameScheduledEmailNotPending    = "scheduled_email_not_pending"
	ErrorNameSchedulingNotIncluded       = "scheduling_not_included"
	ErrorNameTemplateNotFound            = "template_not_found"
	ErrorNameTemplateNotPublished        = "template_not_published"
	ErrorNameTopicDisabled               = "topic_disabled"
	ErrorNameTopicNotFound               = "topic_not_found"
	ErrorNameUnsupportedMediaType        = "unsupported_media_type"
	ErrorNameValidationError             = "validation_error"
)

// APIError is an error returned by the API as a {name, message, statusCode} envelope.
//
// Use errors.As to reach the fields, and errors.Is against a sentinel to branch on the category.
type APIError struct {
	// ErrorName is the machine-readable name from the envelope, e.g. quota_exceeded.
	ErrorName string
	// Message is the human-readable message.
	Message string
	// StatusCode is the HTTP status code.
	StatusCode int
	// Body is the decoded response body, when available.
	Body map[string]any
	// RetryAfter is the Retry-After header in seconds, when the server sent one.
	RetryAfter int
	// RequestID is the server's request id, to quote to support.
	RequestID string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.ErrorName != "":
		return e.ErrorName
	default:
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
}

// Is reports whether this error belongs to the category named by target, so callers can write
// errors.Is(err, ErrRateLimit) without knowing the status-to-category table. Every API error
// also matches ErrAPI and, like every error from this package, ErrMailkube.
func (e *APIError) Is(target error) bool {
	if target == ErrAPI || target == ErrMailkube {
		return true
	}
	return sentinelFor(e.StatusCode) == target
}

// sentinelFor returns the category sentinel for an HTTP status.
func sentinelFor(status int) error {
	if sentinel, ok := statusSentinels[status]; ok {
		return sentinel
	}
	if status >= 500 {
		return ErrServer
	}
	return ErrAPI
}
