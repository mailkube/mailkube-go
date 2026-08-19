//go:build ignore

// Runnable documentation: attach name/value tags to a send.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_tags.go recipient@example.com
//
// Tags are your own labels for a message; they come back on every webhook event the message
// generates, which is what makes them useful for attributing engagement to a campaign.
//
// The server's limits: at most 20 tags, names unique within a message, name at most 16 characters
// and value at most 32, both drawn from [A-Za-z0-9_-]. A value may be empty; the name may not.
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
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_tags.go <recipient@example.com>")
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
		Subject: "Tagged for reporting",
		HTML:    "<p>These tags come back on every webhook this message produces.</p>",
		Text:    "These tags come back on every webhook this message produces.",
		Tags: []mailkube.Tag{
			{Name: "campaign", Value: "onboarding"},
			{Name: "variant", Value: "b"},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("accepted %s, tagged campaign=onboarding variant=b\n", email.ID)
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
