package mailkube

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Attachment is a file attached to an email.
type Attachment struct {
	// Filename is the name of the attached file.
	Filename string
	// Content is the raw file content; the SDK base64-encodes it for the wire.
	Content []byte
	// ContentType is the MIME type, inferred from the filename when empty.
	ContentType string
}

// MarshalJSON renders the attachment for the wire, base64-encoding the raw content.
//
// The encoding lives on the type rather than in the send-body builder so no future verb that
// carries an attachment can forget it.
func (a Attachment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Filename    string `json:"filename"`
		Content     string `json:"content"`
		ContentType string `json:"content_type,omitempty"`
	}{
		Filename:    a.Filename,
		Content:     base64.StdEncoding.EncodeToString(a.Content),
		ContentType: a.ContentType,
	})
}

// Tag is a free-form name/value tag attached to an outgoing email.
//
// Tags are forwarded to the server, which denormalizes them onto the sending log so you can
// filter, export and dashboard sends by tag, and so they ride along on delivery webhooks. Tag
// values are not encrypted, so do not put personal data in them.
//
// The same type describes a tag in both directions: outbound on a send, and inbound on a
// scheduled email or a webhook event.
type Tag struct {
	// Name is the tag name.
	Name string `json:"name"`
	// Value is the tag value; it may be empty, and the key is always sent.
	Value string `json:"value"`
}

// SendEmailParams are the parameters for EmailsService.Send.
//
// A send carries either raw content (HTML and/or Text) or a saved template (TemplateID).
// IdempotencyKey travels as the Idempotency-Key header rather than in the body. An empty
// optional field is omitted from the request entirely rather than sent as null.
type SendEmailParams struct {
	// From is the sender address, optionally with a display name. Required.
	From string
	// To is the recipient address list. Required.
	To []string
	// Subject is the subject line. Required.
	Subject string
	// HTML is the HTML body, for a raw-content send.
	HTML string
	// Text is the plain-text body, for a raw-content send.
	Text string
	// CC are carbon-copy recipients.
	CC []string
	// BCC are blind carbon-copy recipients.
	BCC []string
	// ReplyTo are the Reply-To addresses.
	ReplyTo []string
	// Headers are custom message headers, e.g. In-Reply-To for threading.
	Headers map[string]string
	// Attachments are the file attachments.
	Attachments []Attachment
	// Tags are free-form name/value tags forwarded to the server.
	Tags []Tag
	// TemplateID is the UUID of a saved template to render instead of raw content.
	TemplateID string
	// TemplateVersion is a template version number, or "latest".
	TemplateVersion string
	// Variables are the values for the template's placeholders.
	Variables map[string]string
	// Topic is the mailing-list topic slug this send is attributed to.
	Topic string
	// IdempotencyKey is sent as the Idempotency-Key header.
	IdempotencyKey string
	// ScheduledAt schedules the send instead of delivering it now.
	ScheduledAt time.Time
	// BatchID groups several scheduled sends. Only valid alongside ScheduledAt.
	BatchID string
}

// Email is the result of a successful send.
//
// A scheduled send is acknowledged with 202 and a richer body; Status, ScheduledAt and BatchID
// are populated only then, and IsScheduled reports which shape this is. An immediate send
// leaves all three empty.
//
// This is the worked example of the contract's "widen, never union" rule: one call can return
// two shapes, and adding optional fields plus a boolean keeps every existing caller compiling,
// where a second return type would not.
//
// Timestamps stay the verbatim ISO-8601 strings the server sent; the SDK does not reinterpret
// server data. Parse them with time.Parse(time.RFC3339, ...) if you need a time.Time.
type Email struct {
	// ID is the accepted message's UUID.
	ID string `json:"id"`
	// MessageID is the RFC Message-ID assigned to the message, when the deployment returns one.
	MessageID string `json:"message_id"`
	// IdempotentReplayed is true when this response replays an earlier identical request.
	// It comes from a response header, not the body.
	IdempotentReplayed bool `json:"-"`
	// Status is the scheduled email's status, on a scheduled ack only.
	Status string `json:"status"`
	// ScheduledAt is when the send is due, on a scheduled ack only.
	ScheduledAt string `json:"scheduled_at"`
	// BatchID is the batch label the send was grouped under, on a scheduled ack only.
	BatchID string `json:"batch_id"`
}

// IsScheduled reports whether the send was accepted for later delivery rather than sent now.
func (e *Email) IsScheduled() bool {
	return e.ScheduledAt != ""
}

// ScheduledEmail is a scheduled send that has not been delivered yet.
//
// Timestamps stay verbatim strings, as everywhere else in this package.
type ScheduledEmail struct {
	// ID is the scheduled email's UUID, the same id the send acknowledgement returned.
	ID string `json:"id"`
	// MessageID is the RFC Message-ID the message will carry.
	MessageID string `json:"message_id"`
	// Object is the resource discriminator, always "scheduled_email".
	Object string `json:"object"`
	// Status is one of scheduled, canceled, sent or failed.
	Status string `json:"status"`
	// ScheduledAt is when the send is due.
	ScheduledAt string `json:"scheduled_at"`
	// CreatedAt is when the send was accepted.
	CreatedAt string `json:"created_at"`
	// BatchID is the batch label this send was grouped under, if any.
	BatchID string `json:"batch_id"`
	// Subject is the message subject.
	Subject string `json:"subject"`
	// Recipients is a summary string, not a list: the first recipient plus an overflow count
	// (for example "a@b.com +2"). The full recipient list stays server-side with the frozen
	// payload.
	Recipients string `json:"recipients"`
	// Topic is the mailing-list topic slug the send is attributed to, if any.
	Topic string `json:"topic"`
	// Tags are the message tags attached at send time.
	Tags []Tag `json:"tags"`
}

// PageSteps links to the pages adjacent to the one in hand.
//
// The server omits a step at either end of the range rather than sending null, so an absent
// link and an empty string mean the same thing: there is no such page.
type PageSteps struct {
	// Next is the absolute URL of the following page, empty on the last page.
	Next string `json:"next"`
	// Previous is the absolute URL of the preceding page, empty on the first page.
	Previous string `json:"previous"`
}

// Pagination is the page-number metadata of a listing.
type Pagination struct {
	// Steps links to the adjacent pages.
	Steps PageSteps `json:"steps"`
	// TotalCount is the number of matching records across every page.
	TotalCount int `json:"total_count"`
	// CurrentPage is the 1-based number of the page in hand.
	CurrentPage int `json:"current_page"`
}

// ScheduledEmailPage is one page of scheduled emails.
//
// Use ScheduledEmailsService.All to walk every page; reach for Pagination.Steps.Next only when
// you are paging by hand.
type ScheduledEmailPage struct {
	// Pagination is the page metadata, including the adjacent-page links.
	Pagination Pagination `json:"pagination"`
	// Data are the scheduled emails on this page.
	Data []ScheduledEmail `json:"data"`
}

// HasMore reports whether the server offered a link to a following page.
func (p *ScheduledEmailPage) HasMore() bool {
	return p.Pagination.Steps.Next != ""
}

// CanceledScheduledEmail acknowledges a single scheduled-email cancellation.
type CanceledScheduledEmail struct {
	// ID is the canceled scheduled email's UUID.
	ID string `json:"id"`
	// Object is the resource discriminator, always "scheduled_email".
	Object string `json:"object"`
	// Status is the resulting status, always "canceled".
	Status string `json:"status"`
}

// ScheduledEmailBatchCancel is the result of canceling a whole batch.
type ScheduledEmailBatchCancel struct {
	// Object is the resource discriminator, always "scheduled_email.batch".
	Object string `json:"object"`
	// BatchID is the batch that was targeted.
	BatchID string `json:"batch_id"`
	// CanceledCount is how many pending emails the cancellation affected. An unknown batch is
	// a no-op reporting 0, not an error.
	CanceledCount int `json:"canceled_count"`
}

// ScheduledEmailBatchUpdate is the result of rescheduling a whole batch.
type ScheduledEmailBatchUpdate struct {
	// Object is the resource discriminator, always "scheduled_email.batch".
	Object string `json:"object"`
	// BatchID is the batch that was targeted.
	BatchID string `json:"batch_id"`
	// RescheduledCount is how many pending emails were moved. An unknown batch is a no-op
	// reporting 0, not an error.
	RescheduledCount int `json:"rescheduled_count"`
	// ScheduledAt is the new due time applied to every moved email.
	ScheduledAt string `json:"scheduled_at"`
}

// ScheduledEmailListParams filters ScheduledEmailsService.List and All.
//
// Every filter is optional and a zero-valued one is left out of the query string entirely. The
// listing is scoped server-side to a rolling window around now.
type ScheduledEmailListParams struct {
	// Status narrows to one or several statuses. Only scheduled, canceled and failed can be
	// listed: a sent email has left the collection, so "sent" is a validation error rather
	// than an empty result.
	Status []string
	// BatchID narrows to the emails grouped under one batch label.
	BatchID string
	// ScheduledAtGTE narrows to emails due at or after this instant.
	ScheduledAtGTE time.Time
	// ScheduledAtLTE narrows to emails due at or before this instant.
	ScheduledAtLTE time.Time
	// Page is the 1-based page number to fetch. Zero means the first page.
	Page int
}

// ScheduledEmailUpdateParams are the parameters for rescheduling one scheduled email.
type ScheduledEmailUpdateParams struct {
	// ScheduledAt is the new due time. Same rule as on a send: in the future, within the
	// scheduling horizon. Required.
	ScheduledAt time.Time
	// BatchID optionally moves the email into a batch at the same time.
	BatchID string
}

// ScheduledEmailBatchUpdateParams are the parameters for rescheduling a whole batch.
//
// There is deliberately no BatchID here: the batch is identified by the path, and the server
// drops a body batch_id rather than acting on it, so exposing the field would let a caller
// believe they had re-labelled a batch when nothing happened.
type ScheduledEmailBatchUpdateParams struct {
	// ScheduledAt is the new due time applied to every pending email in the batch. Required.
	ScheduledAt time.Time
}
