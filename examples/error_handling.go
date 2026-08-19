//go:build ignore

// Runnable documentation: the errors you will actually hit, and how to tell them apart.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/error_handling.go recipient@example.com
//
// Every API failure is an *APIError wrapped so that errors.Is matches a category sentinel
// (ErrNotFound, ErrRateLimit, ErrInvalidRequest, ...) and errors.As reaches the fields. Branch on
// APIError.ErrorName — the server's stable machine-readable name — never on the message text,
// which is free to change.
//
// Nothing here sends a message: each call is designed to be refused.
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

var failures int

// expect runs call and reports whether it failed with the wanted error name.
func expect(label, want string, call func() error) {
	err := call()
	if err == nil {
		fmt.Fprintf(os.Stderr, "BAD  %s: expected %s, but the call succeeded\n", label, want)
		failures++
		return
	}
	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) {
		fmt.Fprintf(os.Stderr, "BAD  %s: not an *APIError: %v\n", label, err)
		failures++
		return
	}
	mark := "ok  "
	if apiErr.ErrorName != want {
		mark = "BAD "
		failures++
	}
	fmt.Printf("%s %s: %s (%d)\n", mark, label, apiErr.ErrorName, apiErr.StatusCode)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/error_handling.go <recipient@example.com>")
		os.Exit(2)
	}
	to := []string{os.Args[1]}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()

	// A message with no body at all: HTML, Text and TemplateID are mutually required-one-of.
	expect("missing body", mailkube.ErrorNameValidationError, func() error {
		_, err := client.Emails.Send(ctx, mailkube.SendEmailParams{From: sender(), To: to, Subject: "No body"})
		return err
	})

	// ScheduledAt must be strictly in the future.
	expect("past ScheduledAt", mailkube.ErrorNameValidationError, func() error {
		_, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
			From: sender(), To: to, Subject: "Yesterday", Text: "...",
			ScheduledAt: time.Now().Add(-time.Minute),
		})
		return err
	})

	// BatchID is a grouping label for scheduled sends and means nothing without ScheduledAt.
	expect("BatchID without ScheduledAt", mailkube.ErrorNameValidationError, func() error {
		_, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
			From: sender(), To: to, Subject: "Ungrouped", Text: "...", BatchID: "b1",
		})
		return err
	})

	// A sent email has left the scheduled collection, so filtering for it is a contract error
	// rather than an empty page — the distinction tells you your assumption was wrong.
	expect(`list status "sent"`, mailkube.ErrorNameValidationError, func() error {
		_, err := client.ScheduledEmails.List(ctx, mailkube.ScheduledEmailListParams{Status: []string{"sent"}})
		return err
	})

	// A bad key is refused identically whether it is malformed, unknown or absent, so nothing
	// about the key space leaks.
	expect("bad api key", mailkube.ErrorNameInvalidAPIKey, func() error {
		anonymous, err := mailkube.New(mailkube.WithAPIKey("mk_notarealkey_" + "0000000000000000000000000000000000000000000000000000000000000000"))
		if err != nil {
			return err
		}
		_, err = anonymous.Emails.Send(ctx, mailkube.SendEmailParams{From: sender(), To: to, Subject: "Nope", Text: "..."})
		return err
	})

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "%d case(s) did not behave as documented\n", failures)
		os.Exit(1)
	}
	fmt.Println("all error cases behaved as documented")
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
