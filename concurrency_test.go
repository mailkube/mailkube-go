package mailkube_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mailkube "github.com/mailkube/mailkube-go"
)

// echoClient stubs the transport for the concurrency test.
//
// It deliberately does not reuse stubClient from client_test.go. That helper records the last
// request into shared fields, so driving it from many goroutines would make the *helper* race and
// -race would fail on the test rather than on the client. This one keeps no shared mutable state
// beyond an atomic counter, and answers every request from that request alone.
func echoClient(t *testing.T, calls *atomic.Int64) *mailkube.Client {
	t.Helper()
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		// Echo this request's own distinguishing value back as the resource id, so a caller that
		// receives somebody else's response can be detected. A send is identified by its
		// idempotency key, a scheduled-email read by the id in its path.
		distinguishing := r.Header.Get("Idempotency-Key")
		if distinguishing == "" {
			distinguishing = path.Base(r.URL.Path)
		}
		body := fmt.Sprintf(`{"id":%q}`, distinguishing)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{},
		}, nil
	})}

	client, err := mailkube.New(
		mailkube.WithAPIKey("mk_test"),
		mailkube.WithHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// TestClientIsSafeForConcurrentUse is the concurrency obligation from .rules/SDK_CONTRACT.md.
//
// It asserts more than "no error was returned": every goroutine must receive the response to its
// own request. A client sharing one mutable connection across callers would not usually crash —
// callers would interleave on the socket and receive each other's bodies, which inside a single
// process is a confidentiality bug rather than a flaky test. Only the identity assertion below
// catches that, which is why the contract requires it by name.
//
// Combined with -race (which CI always passes), this covers both failure modes: torn shared state
// and crossed responses.
//
// To confirm the test does what it claims, break the client deliberately — give it one shared
// request or response field — and watch this fail while the rest of the suite stays green.
func TestClientIsSafeForConcurrentUse(t *testing.T) {
	const goroutines = 32

	var calls atomic.Int64
	client := echoClient(t, &calls)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()

			key := fmt.Sprintf("idem-%03d", i)

			// Half the goroutines send and half read a scheduled email: two verbs, two
			// transport paths (sendEmail and fetch), one client.
			var id string
			var err error
			if i%2 == 0 {
				params := minimal()
				params.IdempotencyKey = key

				var email *mailkube.Email
				if email, err = client.Emails.Send(context.Background(), params); email != nil {
					id = email.ID
				}
			} else {
				var item *mailkube.ScheduledEmail
				if item, err = client.ScheduledEmails.Get(context.Background(), key); item != nil {
					id = item.ID
				}
			}
			if err != nil {
				errs <- fmt.Errorf("%s: %w", key, err)
				return
			}
			if id != key {
				errs <- fmt.Errorf("%s: received the response for %q: concurrent calls observed each other", key, id)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if got := calls.Load(); got != goroutines {
		t.Errorf("transport saw %d calls, want %d", got, goroutines)
	}
}

// TestVersionIsStableAcrossGoroutines guards the memoization behind Version.
//
// defaultHeaders calls Version on every request, so it is read concurrently by definition. The
// sync.OnceValue wrapper must resolve exactly one value and hand the same one to every caller.
func TestVersionIsStableAcrossGoroutines(t *testing.T) {
	const goroutines = 16

	var wg sync.WaitGroup
	seen := make([]string, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seen[i] = mailkube.Version()
		}()
	}
	wg.Wait()

	for i, got := range seen {
		if got == "" {
			t.Fatalf("goroutine %d saw an empty version", i)
		}
		if got != seen[0] {
			t.Errorf("goroutine %d saw %q, want %q", i, got, seen[0])
		}
	}
}
