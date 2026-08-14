//go:build ignore

// Runnable documentation: a complete webhook receiver on the standard library alone.
//
//	export MAILKUBE_WEBHOOK_SECRET=whsec_...
//	go run examples/webhook_receiver_http.go        # listens on :8080/webhooks
//
// Expose it (a tunnel is fine for development) and register the URL as a webhook endpoint. Two
// things matter and both are handled below:
//
//  1. Registration. Before the platform saves an endpoint it probes the URL with
//     GET ?hub.mode=subscribe&hub.challenge=<hex> and requires the challenge echoed back. Skip
//     it and the endpoint is never created, so no event is ever delivered: signature
//     verification is necessary but not sufficient on its own. WebhookHandler answers the probe
//     for you, but only if you mount it on GET as well as POST.
//
//  2. Acknowledging fast. A delivery gets ten seconds, only a 2xx counts as accepted, and an
//     endpoint whose acceptance rate falls below 60% over 24 hours is disabled automatically.
//     So OnEvent hands the work to a queue and returns.
//
// Mounting the same handler in a framework:
//
//	r.Match([]string{"GET", "POST"}, "/webhooks", gin.WrapH(handler))  // gin
//	e.Any("/webhooks", echo.WrapHandler(handler))                      // echo
//	app.All("/webhooks", adaptor.HTTPHandler(handler))                 // fiber
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mailkube/mailkube-go"
)

func main() {
	secret := os.Getenv("MAILKUBE_WEBHOOK_SECRET")
	if secret == "" {
		fmt.Fprintln(os.Stderr, "set MAILKUBE_WEBHOOK_SECRET to your endpoint's signing secret")
		os.Exit(2)
	}

	work := make(chan *mailkube.Event, 128)
	go process(work)

	handler := mailkube.WebhookHandler{
		Secret:  secret,
		OnEvent: enqueue(work),
		OnError: func(w http.ResponseWriter, r *http.Request, err error) {
			// Log the failure, then answer as the handler would have. Nothing else writes to
			// w once OnError is set.
			log.Printf("webhook rejected: %v", err)
			http.Error(w, "rejected", http.StatusBadRequest)
		},
	}

	mux := http.NewServeMux()
	mux.Handle("/webhooks", handler) // one route, both methods
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	log.Println("listening on :8080/webhooks")
	log.Fatal(server.ListenAndServe())
}

// enqueue accepts a delivery and returns immediately, deduplicating on the delivery id.
func enqueue(work chan<- *mailkube.Event) func(context.Context, *mailkube.Event) error {
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
	)

	return func(_ context.Context, event *mailkube.Event) error {
		// Event.ID is stable across all 15 retries of one delivery, so it is the dedup key.
		// A real receiver keeps this in a store with a TTL rather than in memory.
		mu.Lock()
		duplicate := seen[event.ID]
		seen[event.ID] = true
		mu.Unlock()

		if duplicate {
			log.Printf("ignoring duplicate delivery %s", event.ID)
			return nil
		}

		select {
		case work <- event:
			return nil
		default:
			// Returning an error answers 500 and the platform retries with backoff, which is
			// the right response to a full queue.
			return fmt.Errorf("queue is full")
		}
	}
}

// process does the real work, off the request path.
func process(work <-chan *mailkube.Event) {
	for event := range work {
		switch data := event.Data.(type) {
		case *mailkube.DeliveredData:
			log.Printf("delivered %s to %s", data.EmailID, data.Delivery.Recipient)
		case *mailkube.BouncedData:
			log.Printf("bounced %s: %d %s", data.EmailID, data.Bounce.Code, data.Bounce.Reason)
		case *mailkube.ClickedData:
			log.Printf("clicked %s: %s", data.EmailID, data.Click.Link)
		case *mailkube.DomainStatusData:
			log.Printf("domain %s is now %s (was %s)", data.Domain, data.Status, data.Previous.Status)
		case *mailkube.UnknownData:
			// A type this SDK version does not model, or one whose payload changed shape.
			// Never an error: forward event.Raw, which keeps every field verbatim.
			log.Printf("unmodelled event %s: %v", event.Type, data.Fields)
		default:
			log.Printf("event %s", event.Type)
		}
	}
}
