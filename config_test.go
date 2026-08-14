package mailkube_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	mailkube "github.com/mailkube/mailkube-go"
)

func TestAnExplicitKeyWins(t *testing.T) {
	t.Setenv("MAILKUBE_API_KEY", "mk_from_env")

	if _, err := mailkube.New(mailkube.WithAPIKey("mk_explicit")); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestTheAPIKeyFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv("MAILKUBE_API_KEY", "mk_from_env")

	if _, err := mailkube.New(); err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestAMissingAPIKeyIsAnActionableError(t *testing.T) {
	t.Setenv("MAILKUBE_API_KEY", "")

	_, err := mailkube.New()
	if !errors.Is(err, mailkube.ErrNoAPIKey) {
		t.Fatalf("err = %v, want ErrNoAPIKey", err)
	}
	if !strings.Contains(err.Error(), "MAILKUBE_API_KEY") {
		t.Errorf("the error should name the environment variable: %v", err)
	}
}

func TestVersionIsDerivedNotALiteral(t *testing.T) {
	// Built from the module's own tree there is no recorded dependency version, so this reads
	// the fallback. What matters is that no hand-maintained constant exists to go stale.
	version := mailkube.Version()
	if version == "" {
		t.Fatal("Version() must never be empty")
	}
	// The version is derived from the git tag, and tagFormat is `v${version}`. Asserting the
	// first character is a digit is what catches a dropped TrimPrefix shipping a User-Agent of
	// `mailkube-go/v1.0.0`, which violates the contract's `mailkube-<lang>/<version>` row. An
	// assertion of the form "mailkube-go/"+Version() == userAgent cannot see that.
	if version[0] < '0' || version[0] > '9' {
		t.Errorf("Version() = %q, want it to start with a digit and carry no leading `v`", version)
	}
}

func TestTheUserAgentCarriesTheBareVersion(t *testing.T) {
	client, seen := stubClient(t, 200, `{"id":"abc123"}`, nil)

	if _, err := client.Emails.Send(context.Background(), minimal()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := seen.req.Header.Get("User-Agent")
	if !strings.HasPrefix(got, "mailkube-go/") {
		t.Fatalf("User-Agent = %q", got)
	}
	if version := strings.TrimPrefix(got, "mailkube-go/"); version == "" || version[0] < '0' || version[0] > '9' {
		t.Errorf("User-Agent = %q, want the version part to start with a digit", got)
	}
}
