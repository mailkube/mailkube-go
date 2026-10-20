//go:build ignore

// List the sends still waiting to go out: one page for the metadata, then the lazy walk for the
// rows. Read-only: nothing is scheduled, moved or canceled.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/list_scheduled_emails.go
//
// List returns one page plus its pagination block. All follows the links the server issues and
// fetches the next page only once the loop asks for it, so the break below means the remaining
// pages are never requested at all.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mailkube/mailkube-go"
)

// shown is how many rows the walk prints before it stops.
const shown = 20

func main() {
	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	filters := mailkube.ScheduledEmailListParams{Status: []string{"scheduled"}}

	first, err := client.ScheduledEmails.List(ctx, filters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("page %d: %d of %d scheduled, more: %t\n",
		first.Pagination.CurrentPage, len(first.Data), first.Pagination.TotalCount, first.HasMore())

	count := 0
	for email, err := range client.ScheduledEmails.All(ctx, filters) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(email.ScheduledAt, email.ID, email.BatchID, email.Subject)
		count++
		if count == shown {
			fmt.Printf("stopped after %d; later pages were never fetched\n", shown)
			break
		}
	}
}
