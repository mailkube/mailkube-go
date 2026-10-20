//go:build ignore

// Describe the API key this client authenticates with: its domain, organization, plan and
// send-quota usage. Nothing is sent, and the key itself is never printed.
//
//	export MAILKUBE_API_KEY=mk_...
//	go run examples/credential_info.go
//
// The key is checked exactly as a send checks it, so this is also how to verify a key before
// using it: a key that cannot send fails with ErrAuthentication (invalid_api_key). The call
// counts as a use of the key, and plan figures can be up to 30 seconds old.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mailkube/mailkube-go"
)

func main() {
	client, err := mailkube.New() // reads MAILKUBE_API_KEY
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	key, err := client.Credential.Info(context.Background())
	if err != nil {
		if errors.Is(err, mailkube.ErrAuthentication) {
			fmt.Fprintln(os.Stderr, "this key cannot send:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("key %s (%s) sends for %s\n", key.KeyID, key.Name, key.Domain.Name)
	fmt.Printf("organization %s (%s)\n", key.Organization.Name, key.Organization.Alias)
	fmt.Println("allowed from", allowedFrom(key.AllowedSourceIPs))

	// A plan without a daily limit lists the monthly quota only; an absent quota is no limit.
	if key.Plan == nil {
		fmt.Println("no subscription")
		return
	}
	fmt.Printf("plan %s (%s)\n", key.Plan.Name, key.Plan.Status)
	for _, quota := range key.Plan.Quotas {
		fmt.Printf("  %s: %d of %d recipients in %s\n", quota.Name, quota.Used, quota.Limit, quota.Period)
	}
}

// allowedFrom renders the key's allowlist, where an empty list means any address.
func allowedFrom(networks []string) string {
	if len(networks) == 0 {
		return "any address"
	}
	return strings.Join(networks, ", ")
}
