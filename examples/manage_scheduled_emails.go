//go:build ignore

// Runnable documentation: list, paginate, retrieve, reschedule and cancel scheduled sends.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/manage_scheduled_emails.go recipient@example.com
//
// The example schedules its own email under a unique batch id and then works only inside that
// batch. That is deliberate: an unfiltered List/All walks every pending send on the account,
// which on a real account means paging through thousands of rows — enough to trip the read rate
// limit — and then mutating whichever message happened to come back first. Scoping to a batch you
// just created keeps the example bounded, repeatable, and safe to run against a live key.
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
		fmt.Fprintln(os.Stderr, "usage: go run examples/manage_scheduled_emails.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	batchID := fmt.Sprintf("example-manage-%d", time.Now().Unix())

	created, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
		From:        sender(),
		To:          []string{os.Args[1]},
		Subject:     "Scheduled for management",
		HTML:        "<p>This one exists to be listed, moved and canceled.</p>",
		ScheduledAt: time.Now().Add(time.Hour),
		BatchID:     batchID,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("scheduled %s in batch %s\n", created.ID, batchID)

	// Reads are rate-limited (60/minute by default), so pace a program that walks pages rather
	// than relying on catching the 429.
	time.Sleep(600 * time.Millisecond)

	filters := mailkube.ScheduledEmailListParams{Status: []string{"scheduled"}, BatchID: batchID}

	// One page, plus the metadata. Every filter is optional: the zero-valued params struct
	// lists everything in the window.
	page, err := client.ScheduledEmails.List(ctx, filters)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d scheduled emails, showing page %d\n", page.Pagination.TotalCount, page.Pagination.CurrentPage)
	time.Sleep(600 * time.Millisecond)

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

	time.Sleep(600 * time.Millisecond)
	manage(ctx, client, created.ID)
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
	time.Sleep(600 * time.Millisecond)

	moved, err := client.ScheduledEmails.Update(ctx, emailID, mailkube.ScheduledEmailUpdateParams{
		ScheduledAt: time.Now().Add(24 * time.Hour),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("rescheduled to %s\n", moved.ScheduledAt)
	time.Sleep(600 * time.Millisecond)

	canceled, err := client.ScheduledEmails.Cancel(ctx, emailID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s is now %s\n", canceled.ID, canceled.Status)
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
