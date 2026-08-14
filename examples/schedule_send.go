//go:build ignore

// Runnable documentation: schedule a send for later, then read the acknowledgement.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/schedule_send.go you@example.com
//
// The `ignore` build tag keeps this file out of the module: `go build ./...`, `go vet ./...`
// and golangci-lint would otherwise compile it, and `go test ./... -coverprofile` would count
// its statements as uncovered. CI compiles each example explicitly instead, with
// `go build -o /dev/null examples/<file>.go`.
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
		fmt.Fprintln(os.Stderr, "usage: go run examples/schedule_send.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	due := time.Now().Add(2 * time.Hour)

	email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
		From:    "Acme <hello@yourdomain.com>",
		To:      []string{os.Args[1]},
		Subject: "Your weekly digest",
		HTML:    "<p>Scheduled from mailkube-go.</p>",
		// A scheduled send is acknowledged with 202 and a richer body. The instant must be in
		// the future and inside the scheduling horizon; the server is the authority on both.
		ScheduledAt: due,
	})
	if err != nil {
		var apiErr *mailkube.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorName == mailkube.ErrorNameSchedulingNotIncluded {
			fmt.Fprintln(os.Stderr, "this plan does not include scheduled sending")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// IsScheduled discriminates the two shapes one Send call can return: an immediate send
	// leaves Status, ScheduledAt and BatchID empty.
	if !email.IsScheduled() {
		fmt.Printf("sent immediately: %s\n", email.ID)
		return
	}

	fmt.Printf("scheduled %s for %s (status %s)\n", email.ID, email.ScheduledAt, email.Status)
	fmt.Printf("manage it with: go run examples/manage_scheduled_emails.go %s\n", email.ID)
}
