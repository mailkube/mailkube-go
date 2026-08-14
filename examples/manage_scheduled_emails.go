//go:build ignore

// Runnable documentation: list, paginate, retrieve, reschedule and cancel scheduled sends.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/manage_scheduled_emails.go [scheduled-email-id]
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
	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	filters := mailkube.ScheduledEmailListParams{Status: []string{"scheduled"}}

	// One page, plus the metadata. Every filter is optional: the zero-valued params struct
	// lists everything in the window.
	page, err := client.ScheduledEmails.List(ctx, filters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d scheduled emails, showing page %d\n", page.Pagination.TotalCount, page.Pagination.CurrentPage)

	// Every page, lazily: All follows the links the API returns, so abandoning the loop early
	// costs nothing. Ranging over it needs go 1.23 or later in your own go.mod; on an older
	// module, loop on List and page.Pagination.Steps.Next by hand.
	for item, err := range client.ScheduledEmails.All(ctx, filters) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("  %s  %s  %s  %q\n", item.ID, item.Status, item.ScheduledAt, item.Subject)
	}

	if len(os.Args) < 2 {
		fmt.Println("pass a scheduled-email id to reschedule and cancel it")
		return
	}
	manage(ctx, client, os.Args[1])
}

// manage retrieves one scheduled email, moves it, then cancels it.
func manage(ctx context.Context, client *mailkube.Client, emailID string) {
	item, err := client.ScheduledEmails.Get(ctx, emailID)
	if err != nil {
		// A send that has already gone out has left the collection.
		if errors.Is(err, mailkube.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "%s is not a pending scheduled email\n", emailID)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("retrieved %s, due %s, recipients %s\n", item.ID, item.ScheduledAt, item.Recipients)

	moved, err := client.ScheduledEmails.Update(ctx, emailID, mailkube.ScheduledEmailUpdateParams{
		ScheduledAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("rescheduled to %s\n", moved.ScheduledAt)

	canceled, err := client.ScheduledEmails.Cancel(ctx, emailID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s is now %s\n", canceled.ID, canceled.Status)
}
