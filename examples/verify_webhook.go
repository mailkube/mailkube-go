//go:build ignore

// Runnable documentation: verify a webhook signature without running a server.
//
//	go run examples/verify_webhook.go fixture.json [more.json...]
//
// examples/webhook_receiver_http.go shows verification inside an HTTP handler. This one strips
// that away: it feeds captured deliveries straight to Verify so you can see exactly what is
// accepted and what is not. Useful for testing your own handler against saved payloads.
//
// A fixture is JSON: {secret, headers: {...}, body: "<raw body string>", must_verify: bool}.
// The body must be the EXACT bytes the server sent — re-serializing parsed JSON will not
// reproduce the signature, which is the single most common integration bug.
//
// See examples/schedule_send.go for why the `ignore` build tag is here.
package main

import (
	"encoding/json"
	"fmt"
	"net/textproto"
	"os"
	"time"

	"github.com/mailkube/mailkube-go"
)

type fixture struct {
	Name       string            `json:"name"`
	Secret     string            `json:"secret"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
	MustVerify bool              `json:"must_verify"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/verify_webhook.go <fixture.json> [more.json...]")
		os.Exit(2)
	}

	failures := 0
	for _, path := range os.Args[1:] {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var f fixture
		if err := json.Unmarshal(raw, &f); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		// Verify takes a HeaderGetter so it works with any transport. Canonicalize the lookup the
		// way net/http would, so "x-webhook-id" and "X-Webhook-Id" both resolve.
		get := mailkube.HeaderFunc(func(name string) string {
			return f.Headers[textproto.CanonicalMIMEHeaderKey(name)]
		})

		verified, detail := true, ""
		event, err := mailkube.Verify([]byte(f.Body), get, f.Secret, 5*time.Minute)
		if err != nil {
			verified, detail = false, err.Error()
		} else {
			detail = "event " + event.Type
		}

		mark := "ok  "
		if verified != f.MustVerify {
			mark = "BAD "
			failures++
		}
		want := "rejected"
		if f.MustVerify {
			want = "verified"
		}
		got := "rejected"
		if verified {
			got = "verified"
		}
		fmt.Printf("%s %s: %s (expected %s) %s\n", mark, f.Name, got, want, detail)
	}

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "%d fixture(s) did not verify as expected\n", failures)
		os.Exit(1)
	}
	fmt.Println("all fixtures behaved as expected")
}
