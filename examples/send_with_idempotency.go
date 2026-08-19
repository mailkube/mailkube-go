//go:build ignore

// Runnable documentation: retry a send safely with an idempotency key.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_idempotency.go recipient@example.com
//
// There are no built-in retries in this SDK, so retrying is your call — and a naive retry after a
// timeout can send the same message twice, because a request that never returned may still have
// succeeded. An idempotency key makes the retry safe: the server remembers the first response for
// that key (24 hours by default) and replays it byte for byte instead of sending again.
//
// The key is fingerprinted against the request body. Reusing a key with a DIFFERENT body is an
// error rather than a silent replay, which is what stops a recycled key from swallowing a real
// second message.
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
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
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_idempotency.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()

	// In real code this is a stable id for the thing you are sending about — an order id, a job
	// id — not a random value, otherwise a retry generates a new key and sends twice.
	key := fmt.Sprintf("order-%d", time.Now().Unix())

	params := mailkube.SendEmailParams{
		From:           sender(),
		To:             []string{os.Args[1]},
		Subject:        "Sent at most once",
		HTML:           "<p>Retrying this send cannot duplicate it.</p>",
		Text:           "Retrying this send cannot duplicate it.",
		IdempotencyKey: key,
	}

	first, err := client.Emails.Send(ctx, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("first  call: %s\n", first.ID)

	// Pretend the first response never reached us and we retried.
	replay, err := client.Emails.Send(ctx, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("replayed   : %s\n", replay.ID)

	if first.ID != replay.ID {
		fmt.Fprintf(os.Stderr, "expected the same id back, got %s then %s — that is a second send\n", first.ID, replay.ID)
		os.Exit(1)
	}
	fmt.Println("same id returned: the retry was replayed, not resent")

	// Same key, different body: refused rather than replayed.
	changed := params
	changed.Subject = "A different message entirely"
	_, err = client.Emails.Send(ctx, changed)
	var apiErr *mailkube.APIError
	switch {
	case err == nil:
		fmt.Fprintln(os.Stderr, "expected a reused key with a changed body to be rejected")
		os.Exit(1)
	case errors.As(err, &apiErr):
		fmt.Printf("key reuse with a changed body correctly rejected: %s\n", apiErr.ErrorName)
	default:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// sender returns the verified address this account may send from. Override per
// environment; the fallback is a placeholder and will be rejected until you set your own
// domain.
func sender() string {
	if from := os.Getenv("MAILKUBE_FROM"); from != "" {
		return from
	}
	return "Acme <hello@yourdomain.com>"
}
