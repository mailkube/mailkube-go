package mailkube

import "context"

// emailsPath is the send endpoint, relative to the base URL.
const emailsPath = "emails"

// EmailsService is the emails namespace, reached as client.Emails.
//
// This is the worked example every new service copies. Note what it does not do: it holds no
// configuration, performs no I/O, and never imports net/http. It depends only on the narrowest
// transport interface its verbs need.
type EmailsService struct {
	transport sendTransport
}

// Send sends an email.
//
// Supply HTML and/or Text for a raw send, or TemplateID for a saved template.
// IdempotencyKey travels as the Idempotency-Key header rather than in the body. Setting
// ScheduledAt schedules the send instead of delivering it now; the result then reports
// IsScheduled, and the send can be managed through client.ScheduledEmails until it is due.
func (s *EmailsService) Send(ctx context.Context, params SendEmailParams) (*Email, error) {
	return s.transport.sendEmail(ctx, sendSpec(params))
}

// sendEmailBody is the wire body of a send.
//
// A tagged struct rather than a hand-built map: `omitempty` states per field whether an unset
// value is absent from the wire or sent explicitly, which is the rule that used to live in a
// type switch. Adding a send parameter is one field here and one field on SendEmailParams.
type sendEmailBody struct {
	From            string            `json:"from"`
	To              []string          `json:"to"`
	Subject         string            `json:"subject"`
	HTML            string            `json:"html,omitempty"`
	Text            string            `json:"text,omitempty"`
	CC              []string          `json:"cc,omitempty"`
	BCC             []string          `json:"bcc,omitempty"`
	ReplyTo         []string          `json:"reply_to,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Attachments     []Attachment      `json:"attachments,omitempty"`
	Tags            []Tag             `json:"tags,omitempty"`
	TemplateID      string            `json:"template_id,omitempty"`
	TemplateVersion string            `json:"template_version,omitempty"`
	Variables       map[string]string `json:"variables,omitempty"`
	Topic           string            `json:"topic,omitempty"`
	ScheduledAt     string            `json:"scheduled_at,omitempty"`
	BatchID         string            `json:"batch_id,omitempty"`
}

// sendSpec builds the send request from the caller's parameters.
func sendSpec(params SendEmailParams) requestSpec {
	spec := requestSpec{
		path: emailsPath,
		body: sendEmailBody{
			From:            params.From,
			To:              params.To,
			Subject:         params.Subject,
			HTML:            params.HTML,
			Text:            params.Text,
			CC:              params.CC,
			BCC:             params.BCC,
			ReplyTo:         params.ReplyTo,
			Headers:         params.Headers,
			Attachments:     params.Attachments,
			Tags:            params.Tags,
			TemplateID:      params.TemplateID,
			TemplateVersion: params.TemplateVersion,
			Variables:       params.Variables,
			Topic:           params.Topic,
			ScheduledAt:     renderTime(params.ScheduledAt),
			BatchID:         params.BatchID,
		},
	}
	if params.IdempotencyKey != "" {
		spec.headers = map[string]string{"Idempotency-Key": params.IdempotencyKey}
	}
	return spec
}
