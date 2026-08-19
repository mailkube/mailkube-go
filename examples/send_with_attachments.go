//go:build ignore

// Runnable documentation: attach files to a send.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/send_with_attachments.go recipient@example.com [path/to/file.pdf]
//
// Content is raw bytes; the SDK base64-encodes it for the wire, so you never do that yourself.
// ContentType is inferred from the filename when you leave it empty.
//
// With no path argument the example builds a tiny valid PDF in memory, so it runs without you
// having to find a file first.
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mailkube/mailkube-go"
)

// minimalPDF is the smallest thing a PDF reader will still open, so the example has something
// real to attach.
var minimalPDF = []byte("%PDF-1.4\n" +
	"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
	"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 50]>>endobj\n" +
	"trailer<</Root 1 0 R>>\n%%EOF\n")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/send_with_attachments.go <recipient@example.com> [file]")
		os.Exit(2)
	}

	content := minimalPDF
	filename := "invoice.pdf"
	if len(os.Args) > 2 {
		data, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		content, filename = data, os.Args[2]
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	email, err := client.Emails.Send(context.Background(), mailkube.SendEmailParams{
		From:    sender(),
		To:      []string{os.Args[1]},
		Subject: "Your invoice",
		HTML:    "<p>Your invoice is attached.</p>",
		Attachments: []mailkube.Attachment{
			{Filename: filename, Content: content, ContentType: "application/pdf"},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("accepted %s with 1 attachment (%d bytes)\n", email.ID, len(content))
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
