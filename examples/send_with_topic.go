//go:build ignore

// Runnable documentation: send against a mailing-list topic.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_topic.go recipient@example.com newsletter
//
// A topic is a subscription group your recipients can opt out of individually, and Topic is the
// slug you configured for it (16 characters max). Sending under one means the unsubscribe link
// removes the recipient from that topic rather than from everything you send.
//
// The slug must already exist and be enabled on the sending domain's apex. An unknown or disabled
// slug is rejected outright, BEFORE the message is charged or queued — so a typo costs you
// nothing, but it does not silently fall back to sending untopiced either. The second half of
// this example triggers that rejection on purpose.
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mailkube/mailkube-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_topic.go <recipient@example.com> [topic-slug]")
		os.Exit(2)
	}
	topic := "newsletter"
	if len(os.Args) > 2 {
		topic = os.Args[2]
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()

	email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
		From:    sender(),
		To:      []string{os.Args[1]},
		Subject: fmt.Sprintf("Sent under the %q topic", topic),
		HTML:    "<p>Unsubscribing from this removes you from this topic only.</p>",
		Text:    "Unsubscribing from this removes you from this topic only.",
		Topic:   topic,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("accepted %s under topic %s\n", email.ID, topic)

	// The negative case: a slug that was never configured.
	_, err = client.Emails.Send(ctx, mailkube.SendEmailParams{
		From:    sender(),
		To:      []string{os.Args[1]},
		Subject: "This one never leaves the building",
		Text:    "You should not be reading this.",
		Topic:   "no-such-topic",
	})
	var apiErr *mailkube.APIError
	switch {
	case err == nil:
		fmt.Fprintln(os.Stderr, "expected an unknown topic to be rejected, but it was accepted")
		os.Exit(1)
	case errors.As(err, &apiErr) && apiErr.ErrorName == mailkube.ErrorNameTopicNotFound:
		fmt.Printf("unknown topic correctly rejected: %s\n", apiErr.ErrorName)
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
