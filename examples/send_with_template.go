//go:build ignore

// Runnable documentation: send a saved template instead of raw content.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_template.go recipient@example.com <template-uuid>
//
// A send carries EITHER raw content (HTML/Text) or a template, never both. The template must
// exist on the sending domain and be published — a draft or deleted one is a template_not_found.
// TemplateVersion "latest" tracks the newest version; pin a number to freeze the content you
// tested against.
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
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_template.go <recipient@example.com> <template-uuid>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	email, err := client.Emails.Send(context.Background(), mailkube.SendEmailParams{
		From:            sender(),
		To:              []string{os.Args[1]},
		Subject:         "Welcome aboard",
		TemplateID:      os.Args[2],
		TemplateVersion: "latest",
		Variables:       map[string]string{"first_name": "Ada", "plan": "Pro"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("accepted %s from template %s\n", email.ID, os.Args[2])
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
