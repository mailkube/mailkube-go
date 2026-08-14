package mailkube_test

import (
	"testing"

	mailkube "github.com/mailkube/mailkube-go"
)

// TestTheDocumentedErrorNamesAreAllExported pins the ErrorName* constants against a hand-written
// literal list of the documented names.
//
// The list is deliberately spelled out here rather than derived from the constants: Go cannot
// enumerate a const block at runtime, so a test that compared the constants to themselves would
// pass no matter what they said. Written this way, a misspelled value fails on the comparison and
// a deleted constant fails at compile time, both without needing another SDK checked out. The
// names are the same 30 the other mailkube SDKs expose (mailkube-python's ErrorName enum is the
// reference); add a row here in the same change that adds a constant.
func TestTheDocumentedErrorNamesAreAllExported(t *testing.T) {
	documented := map[string]string{
		"application_error":              mailkube.ErrorNameApplicationError,
		"body_content_rejected":          mailkube.ErrorNameBodyContentRejected,
		"browser_not_allowed":            mailkube.ErrorNameBrowserNotAllowed,
		"concurrent_idempotent_requests": mailkube.ErrorNameConcurrentIdempotentRequest,
		"from_domain_not_allowed":        mailkube.ErrorNameFromDomainNotAllowed,
		"invalid_api_key":                mailkube.ErrorNameInvalidAPIKey,
		"invalid_attachment":             mailkube.ErrorNameInvalidAttachment,
		"invalid_from_address":           mailkube.ErrorNameInvalidFromAddress,
		"invalid_idempotency_key":        mailkube.ErrorNameInvalidIdempotencyKey,
		"invalid_idempotent_request":     mailkube.ErrorNameInvalidIdempotentRequest,
		"invalid_request_body":           mailkube.ErrorNameInvalidRequestBody,
		"link_reputation_blocked":        mailkube.ErrorNameLinkReputationBlocked,
		"max_message_size_exceeded":      mailkube.ErrorNameMaxMessageSizeExceeded,
		"max_recipients_exceeded":        mailkube.ErrorNameMaxRecipientsExceeded,
		"method_not_allowed":             mailkube.ErrorNameMethodNotAllowed,
		"missing_required_field":         mailkube.ErrorNameMissingRequiredField,
		"missing_required_variable":      mailkube.ErrorNameMissingRequiredVariable,
		"missing_user_agent":             mailkube.ErrorNameMissingUserAgent,
		"not_acceptable":                 mailkube.ErrorNameNotAcceptable,
		"quota_exceeded":                 mailkube.ErrorNameQuotaExceeded,
		"rate_limit_exceeded":            mailkube.ErrorNameRateLimitExceeded,
		"scheduled_email_not_found":      mailkube.ErrorNameScheduledEmailNotFound,
		"scheduled_email_not_pending":    mailkube.ErrorNameScheduledEmailNotPending,
		"scheduling_not_included":        mailkube.ErrorNameSchedulingNotIncluded,
		"template_not_found":             mailkube.ErrorNameTemplateNotFound,
		"template_not_published":         mailkube.ErrorNameTemplateNotPublished,
		"topic_disabled":                 mailkube.ErrorNameTopicDisabled,
		"topic_not_found":                mailkube.ErrorNameTopicNotFound,
		"unsupported_media_type":         mailkube.ErrorNameUnsupportedMediaType,
		"validation_error":               mailkube.ErrorNameValidationError,
	}

	if len(documented) != 30 {
		t.Fatalf("the documented error reference lists 30 names, this test lists %d", len(documented))
	}
	for want, got := range documented {
		if got != want {
			t.Errorf("constant for %q holds %q", want, got)
		}
	}
}
