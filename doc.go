// Package mailkube is the Go SDK for mailkube: send transactional email, manage scheduled
// sends, and receive webhooks.
//
// Create one client and reuse it; it is safe for concurrent use.
//
//	client, err := mailkube.New() // reads MAILKUBE_API_KEY
//	if err != nil {
//		return err
//	}
//	email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
//		From:    "Acme <hello@yourdomain.com>",
//		To:      []string{"customer@example.com"},
//		Subject: "Hello world",
//		HTML:    "<p>It works!</p>",
//	})
//
// A send carrying ScheduledAt is accepted but not delivered yet. Until it is due it lives in
// the scheduled-emails collection, one at a time or a whole batch at once:
//
//	page, err := client.ScheduledEmails.List(ctx, mailkube.ScheduledEmailListParams{})
//	for item, err := range client.ScheduledEmails.All(ctx, params) { ... }
//	_, err = client.ScheduledEmails.Cancel(ctx, email.ID)
//	_, err = client.ScheduledEmails.Batches.Cancel(ctx, "launch")
//
// Webhooks need no client. Mount WebhookHandler on GET and POST: the GET is the registration
// handshake the platform probes with before it will save your endpoint, and skipping it means
// no event is ever delivered.
//
//	http.Handle("/webhooks", mailkube.WebhookHandler{Secret: secret, OnEvent: onEvent})
//
// Verify and parse by hand instead when you want to own the response:
//
//	event, err := mailkube.Verify(rawBody, r.Header, secret, 0)
//
// Errors carry a category sentinel, so branch with errors.Is and reach the details with
// errors.As:
//
//	var apiErr *mailkube.APIError
//	if errors.As(err, &apiErr) && errors.Is(err, mailkube.ErrRateLimit) {
//		time.Sleep(time.Duration(apiErr.RetryAfter) * time.Second)
//	}
//
// Everything this package returns matches ErrMailkube.
//
// The conventions every mailkube SDK shares are in .rules/SDK_CONTRACT.md; how they are
// realized in Go is in .rules/SDK_DESIGN.md.
package mailkube
