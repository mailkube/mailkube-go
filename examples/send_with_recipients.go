//go:build ignore

// Runnable documentation: every recipient field and custom headers on one message.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_recipients.go recipient@example.com
//
// To, CC, BCC and ReplyTo are all []string. The account limit is 50 recipients per message,
// counted across To + CC + BCC.
//
// Headers carry your own metadata. The API caps them at 20 per message, header names match
// [A-Za-z0-9-] up to 64 characters, and no value may contain CR or LF.
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mailkube/mailkube-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_recipients.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	email, err := client.Emails.Send(context.Background(), mailkube.SendEmailParams{
		From:    sender(),
		To:      []string{os.Args[1]},
		CC:      []string{os.Args[1]},
		BCC:     []string{os.Args[1]},
		ReplyTo: []string{"support@yourdomain.com"},
		Subject: "Every recipient field at once",
		HTML:    "<p>to, cc, bcc and reply-to on a single message.</p>",
		Text:    "to, cc, bcc and reply-to on a single message.",
		Headers: map[string]string{
			"X-Campaign-Id":   "recipients-demo",
			"X-Customer-Tier": "gold",
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("accepted %s (message-id %s)\n", email.ID, email.MessageID)
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
