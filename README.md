# mailkube-go

[![CI](https://github.com/mailkube/mailkube-go/actions/workflows/ci.yml/badge.svg)](https://github.com/mailkube/mailkube-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mailkube/mailkube-go.svg)](https://pkg.go.dev/github.com/mailkube/mailkube-go)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Code of Conduct](https://img.shields.io/badge/Contributor%20Covenant-2.1-purple.svg)](CODE_OF_CONDUCT.md)

The official Go SDK for mailkube.

## Install

```bash
go get github.com/mailkube/mailkube-go
```

Requires Go 1.23 or later.

## Usage

```go
package main

import (
	"context"
	"log"

	"github.com/mailkube/mailkube-go"
)

func main() {
	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		log.Fatal(err)
	}

	email, err := client.Emails.Send(context.Background(), mailkube.SendEmailParams{
		From:    "Acme <hello@yourdomain.com>",
		To:      []string{"customer@example.com"},
		Subject: "Hello world",
		HTML:    "<p>It works!</p>",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(email.ID, email.MessageID)
}
```

**Zero dependencies.** `go.mod` has no `require` block; the standard library covers everything.

### Configuration

| Option | Function | Environment | Default |
|---|---|---|---|
| API key | `WithAPIKey` | `MAILKUBE_API_KEY` | required |
| Base URL | `WithBaseURL` | `MAILKUBE_BASE_URL` | `https://api.mailkube.com/mta/v1/` |
| Timeout | `WithTimeout` | | 30s |
| HTTP client | `WithHTTPClient` | | `&http.Client{Timeout: ...}` |
| Logger | `WithLogger` | `MAILKUBE_LOG` | silent |

Create one client and reuse it: it is safe for concurrent use from any number of goroutines.
Pass your own `*http.Client` to add a proxy, custom transport or instrumentation.

There are deliberately **no built-in retries**. An error matching `ErrRateLimit` carries
`RetryAfter` and one matching `ErrServer` is safe to retry with backoff, so the calling
application decides. Set `IdempotencyKey` to make a retry safe.

The client this package builds **refuses HTTP redirects**. Go's default policy rewrites a
redirected `PATCH` or `DELETE` into a `GET` and drops the body, which would turn a redirected
cancel into a read that answers `200` and reports a cancellation that never happened.

### Attachments, tags and templates

```go
email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
	From:    "Acme <hello@yourdomain.com>",
	To:      []string{"customer@example.com"},
	Subject: "Your invoice",
	Attachments: []mailkube.Attachment{
		{Filename: "invoice.pdf", Content: pdfBytes}, // base64-encoded for you
	},
	Tags:            []mailkube.Tag{{Name: "campaign", Value: "spring"}},
	TemplateID:      "0f9c...", // instead of HTML/Text
	TemplateVersion: "latest",
	Variables:       map[string]string{"name": "Ada"},
	IdempotencyKey:  "invoice-42",
})
```

## Scheduled emails

A send carrying `ScheduledAt` is accepted but not delivered yet; the acknowledgement reports
`IsScheduled`. Until it is due it lives in the scheduled-emails collection.

```go
email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
	From: "Acme <hello@yourdomain.com>", To: []string{"customer@example.com"},
	Subject: "Your weekly digest", HTML: "<p>Soon.</p>",
	ScheduledAt: time.Now().Add(2 * time.Hour),
	BatchID:     "digest-2026-08", // optional, groups sends so you can move them as a unit
})

item, err := client.ScheduledEmails.Get(ctx, email.ID)
moved, err := client.ScheduledEmails.Update(ctx, email.ID, mailkube.ScheduledEmailUpdateParams{
	ScheduledAt: time.Now().Add(24 * time.Hour),
})
canceled, err := client.ScheduledEmails.Cancel(ctx, email.ID)
```

Timestamps come back as the verbatim strings the server sent. Parse one with
`time.Parse(time.RFC3339, item.ScheduledAt)`.

### Listing

`List` returns one page. `All` walks every page lazily, following the links the API returns, so
abandoning the loop early costs nothing:

```go
filters := mailkube.ScheduledEmailListParams{
	Status:         []string{"scheduled"},
	ScheduledAtGTE: time.Now(),
}

page, err := client.ScheduledEmails.List(ctx, filters)
log.Println(page.Pagination.TotalCount, page.HasMore())

for item, err := range client.ScheduledEmails.All(ctx, filters) {
	if err != nil {
		return err
	}
	log.Println(item.ID, item.Status, item.ScheduledAt)
}
```

Ranging over an iterator needs `go 1.23` or later in **your own** `go.mod`, not just in your
toolchain. On an older module, page by hand:

```go
page, err := client.ScheduledEmails.List(ctx, filters)
for err == nil {
	// ... use page.Data ...
	if !page.HasMore() {
		break
	}
	page, err = client.ScheduledEmails.List(ctx, mailkube.ScheduledEmailListParams{Page: page.Pagination.CurrentPage + 1})
}
```

Only `scheduled`, `canceled` and `failed` can be listed: a sent email has left the collection.

### Batches

```go
moved, err := client.ScheduledEmails.Batches.Update(ctx, "digest-2026-08",
	mailkube.ScheduledEmailBatchUpdateParams{ScheduledAt: time.Now().Add(6 * time.Hour)})
log.Println(moved.RescheduledCount)

canceled, err := client.ScheduledEmails.Batches.Cancel(ctx, "digest-2026-08")
log.Println(canceled.CanceledCount) // an unknown batch is a no-op reporting 0, not an error
```

## Webhooks

`WebhookHandler` is an `http.Handler`, so it mounts anywhere. **Mount it on GET and POST.**
Before the platform saves an endpoint it probes the URL with
`GET ?hub.mode=subscribe&hub.challenge=<hex>` and requires the challenge echoed back verbatim;
an endpoint that fails the probe is never created and no event is ever delivered to it. The
handler answers the probe for you, but only if the probe can reach it.

```go
handler := mailkube.WebhookHandler{
	Secret: os.Getenv("MAILKUBE_WEBHOOK_SECRET"),
	OnEvent: func(ctx context.Context, event *mailkube.Event) error {
		return queue.Publish(ctx, event) // acknowledge fast, work elsewhere
	},
}

http.Handle("/webhooks", handler)                                   // net/http, chi
r.Match([]string{"GET", "POST"}, "/webhooks", gin.WrapH(handler))   // gin
e.Any("/webhooks", echo.WrapHandler(handler))                       // echo
app.All("/webhooks", adaptor.HTTPHandler(handler))                  // fiber
```

`OnEvent` runs on the request's context and the sender allows **ten seconds**. Only a 2xx counts
as accepted, and an endpoint whose acceptance rate falls below 60% over 24 hours is disabled
automatically, so hand the work to a queue and return. Deduplicate on `Event.ID`: it is stable
across all retries of one delivery.

Responses: `204` accepted, `401` bad signature or stale timestamp, `413` oversized body, `400`
not a webhook, `500` when `OnEvent` returns an error. Set `OnError` to own the error response.

### Verifying by hand

```go
raw, _ := io.ReadAll(r.Body)                              // the raw bytes, never re-encoded
event, err := mailkube.Verify(raw, r.Header, secret, 0)   // 0 means the default 300s tolerance
```

`Verify` takes any `HeaderGetter`, and `http.Header` already is one. For fasthttp stacks such as
Fiber, whose `Ctx.Get` is variadic, wrap it:

```go
event, err := mailkube.Verify(raw, mailkube.HeaderFunc(func(name string) string {
	return c.Get(name)
}), secret, 0)
```

### Event types

`Event.Data` holds the payload typed for the event. Anything this version does not model, and
any known event whose payload changed shape, arrives as `*UnknownData` rather than an error.

```go
switch data := event.Data.(type) {
case *mailkube.BouncedData:
	log.Println(data.EmailID, data.Bounce.Code, data.Bounce.Reason)
case *mailkube.ClickedData:
	log.Println(data.EmailID, data.Click.Link)
case *mailkube.UnknownData:
	log.Println("unmodelled", event.Type, data.Fields)
}
```

| `type` | `Data` | Carries |
| --- | --- | --- |
| `email.sent` | `*SentData` | `Sent` — accepted and spooled for transmission |
| `email.delivered` | `*DeliveredData` | `Delivery` — accepted by the receiving server |
| `email.bounced` | `*BouncedData` | `Bounce` — permanent failure, with code and reason |
| `email.delivery_delayed` | `*DelayedData` | `Delay` — transient failure, may still succeed |
| `email.suppressed` | `*SuppressedData` | `Suppression` — recipients dropped before sending |
| `email.scheduled` | `*ScheduledData` | `Scheduled` — accepted for later transmission |
| `email.failed` | `*FailedData` | `Failed` — dropped at dispatch, never transmitted |
| `email.opened` | `*OpenedData` | `Open` — tracking pixel loaded |
| `email.clicked` | `*ClickedData` | `Click` — tracked link followed |
| `domain.status` | `*DomainStatusData` | a sending domain's status or onboarding transition |
| `webhook.status` | `*WebhookStatusData` | a webhook endpoint's status transition |

`mailkube.EventTypes()` returns the same list, derived from the catalogue itself.

Every `email.*` payload embeds `MessageContext` (`EmailID`, `CreatedAt`, `Domain`, `Subject`,
`To`, `From`, `Tags`). The four transaction-derived fields are pointers because the server can
send them as `null`, which is not the same as empty. `Event.Raw` always holds the verbatim body,
so a receiver that forwards events keeps fields this SDK version predates.

On the `Open` and `Click` blocks, `IPAddress`, `Country` and `UserAgent` are recorded only where the
sending domain has elected them, and both settings are off by default. The server omits the key
rather than sending an empty value, so all three read as `""` when nothing was recorded. Go does not
tell that apart from an elected but blank value; a receiver that needs the difference reads
`Event.Raw`. All three carry `omitempty`, so re-marshalling an event never puts the keys back on the
wire for a sender that declined them. `Country` can be empty even where the address was recorded,
because it is resolved at the edge and is not available on every path.

## Errors

Branch on the category with `errors.Is`, and reach the envelope with `errors.As`:

```go
var apiErr *mailkube.APIError
if errors.As(err, &apiErr) && errors.Is(err, mailkube.ErrRateLimit) {
	time.Sleep(time.Duration(apiErr.RetryAfter) * time.Second)
}
```

Categories: `ErrBadRequest` (400), `ErrAuthentication` (403), `ErrNotFound` (404),
`ErrConflict` (409), `ErrInvalidRequest` (422), `ErrRateLimit` (429), `ErrServer` (5xx),
`ErrAPI` (anything else, and a supertype of all of them). A transport failure matches
`ErrConnection` and a 2xx the SDK could not read matches `ErrUnexpectedResponse`; neither is an
API error. Everything this package returns matches `ErrMailkube`.

`APIError.ErrorName` stays a plain string so a name this release has never heard of is reported
verbatim; the documented values are available as `ErrorName*` constants.

Every failed request carries `APIError.RequestID`, the id the API returned for that exact call.
Log it, and quote it when you report a failure to support — it is what lets us find your request:

```go
if errors.As(err, &apiErr) {
	log.Printf("mailkube %s failed: %s (request %s)", apiErr.ErrorName, apiErr.Message, apiErr.RequestID)
}
```

## Logging

Silent by default. Turn request and response logging on with a logger of your own, or with the
`MAILKUBE_LOG` environment variable:

```go
client, err := mailkube.New(mailkube.WithLogger(slog.Default()))
```

```bash
MAILKUBE_LOG=debug ./your-app
```

`Authorization` and `Idempotency-Key` are redacted from log output.

## Examples

Runnable scripts in [`examples/`](examples/). They carry a `//go:build ignore` tag, so run one
with `go run examples/simple_send.go`:

- [`simple_send.go`](examples/simple_send.go) — the smallest useful program
- [`schedule_send.go`](examples/schedule_send.go) — schedule a send, then read the ack
- [`manage_scheduled_emails.go`](examples/manage_scheduled_emails.go) — list, paginate,
  retrieve, reschedule and cancel
- [`schedule_batch.go`](examples/schedule_batch.go) — schedule a batch, then move or cancel it
  as a unit
- [`webhook_receiver_http.go`](examples/webhook_receiver_http.go) — a complete receiver:
  handshake, verify, deduplicate, acknowledge fast

## Extending this SDK

Before adding a verb, a service, a paginated listing or a webhook event, read
[`.rules/SDK_CONTRACT.md`](.rules/SDK_CONTRACT.md) (the decisions every mailkube SDK shares)
and [`.rules/SDK_DESIGN.md`](.rules/SDK_DESIGN.md) (how they are realized in Go, including the
deliberate deviations). Both carry a step-by-step checklist.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup and the quality gates every change
must pass. Security issues: see [SECURITY.md](SECURITY.md).

## License

[Apache-2.0](LICENSE) © 2026 Mail Tactic Corporation
