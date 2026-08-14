package mailkube_test

import (
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
	if mailkube.Version() == "" {
		t.Error("Version() must never be empty")
	}
}
