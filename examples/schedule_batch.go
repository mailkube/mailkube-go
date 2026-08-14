//go:build ignore

// Runnable documentation: schedule several sends as one batch, then move or cancel the batch.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/schedule_batch.go you@example.com
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mailkube/mailkube-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/schedule_batch.go <recipient@example.com>")
		os.Exit(2)
	}

	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx := context.Background()
	// A batch is just a label shared by several scheduled sends. Pick your own; it is how you
	// move or cancel them as a unit later.
	batchID := fmt.Sprintf("digest-%d", time.Now().Unix())
	due := time.Now().Add(3 * time.Hour)

	for i := 1; i <= 3; i++ {
		email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
			From:        "Acme <hello@yourdomain.com>",
			To:          []string{os.Args[1]},
			Subject:     fmt.Sprintf("Digest part %d", i),
			HTML:        fmt.Sprintf("<p>Part %d of 3.</p>", i),
			ScheduledAt: due,
			BatchID:     batchID, // only valid alongside ScheduledAt
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("scheduled %s in batch %s\n", email.ID, email.BatchID)
	}

	// Move every pending email in the batch at once. There is no batch_id in the body: the
	// path identifies the batch.
	moved, err := client.ScheduledEmails.Batches.Update(ctx, batchID, mailkube.ScheduledEmailBatchUpdateParams{
		ScheduledAt: due.Add(time.Hour),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("moved %d emails to %s\n", moved.RescheduledCount, moved.ScheduledAt)

	// Canceling an unknown batch is a no-op reporting 0, not an error.
	canceled, err := client.ScheduledEmails.Batches.Cancel(ctx, batchID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("canceled %d emails in batch %s\n", canceled.CanceledCount, canceled.BatchID)
}
