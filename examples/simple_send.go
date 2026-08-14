//go:build ignore

// Runnable documentation: the smallest useful program this module supports.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/simple_send.go you@example.com
//
// The `ignore` build tag is load-bearing, not decoration. Without it this file joins the
// module: `go build ./...`, `go vet ./...` and golangci-lint would all compile it, and — worse
// — `go test ./... -coverprofile` would count its statements as uncovered and drag the 90%
// gate down for code that exists to be read and run, not shipped. Examples are excluded from
// the duplication gate for the same reason.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mailkube/mailkube-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/simple_send.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
		From:    "Acme <hello@yourdomain.com>",
		To:      []string{os.Args[1]},
		Subject: "Hello from mailkube-go",
		HTML:    "<p>It works!</p>",
		Text:    "It works!",
		// Set an idempotency key on anything you might retry: the API replays the original
		// response instead of sending twice.
		IdempotencyKey: fmt.Sprintf("example-%d", time.Now().Unix()),
	})
	if err != nil {
		// There are no built-in retries on purpose, so the caller decides. A rate-limit error
		// carries the server's own Retry-After.
		var apiErr *mailkube.APIError
		if errors.As(err, &apiErr) && errors.Is(err, mailkube.ErrRateLimit) {
			fmt.Fprintf(os.Stderr, "rate limited; retry after %ds\n", apiErr.RetryAfter)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("accepted %s (message-id %s)\n", email.ID, email.MessageID)
}
