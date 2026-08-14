package mailkube

// Internal tests for the unexported seams: URL building, the error-category table and the
// wire-rendering helpers. Everything reachable through the public API is covered from the
// external _test package instead.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, opts ...Option) *Client {
	t.Helper()
	client, err := New(append([]Option{WithAPIKey("mk_test")}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestWithBaseURLAndWithTimeout(t *testing.T) {
	client := testClient(t, WithBaseURL("https://staging.example.com/v1/"), WithTimeout(5*time.Second))

	if client.baseURL != "https://staging.example.com/v1/" {
		t.Errorf("baseURL = %q", client.baseURL)
	}
	if client.timeout != 5*time.Second || client.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", client.timeout)
	}
}

func TestTheBaseURLFallsBackToTheEnvironment(t *testing.T) {
	t.Setenv(envBaseURL, "https://from-env.example.com/v1/")

	if got := testClient(t).baseURL; got != "https://from-env.example.com/v1/" {
		t.Errorf("baseURL = %q", got)
	}
}

func TestBuildURLJoinsARelativePath(t *testing.T) {
	got, err := testClient(t).buildURL("emails")
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if got != DefaultBaseURL+"emails" {
		t.Errorf("url = %q", got)
	}
}

func TestBuildURLAcceptsASameOriginAbsoluteURL(t *testing.T) {
	link := DefaultBaseURL + "emails?page=2"

	got, err := testClient(t).buildURL(link)
	if err != nil {
		t.Fatalf("buildURL: %v", err)
	}
	if got != link {
		t.Errorf("url = %q", got)
	}
}

func TestBuildURLRefusesALinkOffTheConfiguredOrigin(t *testing.T) {
	// Every request carries the API key, so a foreign host must never be followed.
	_, err := testClient(t).buildURL("https://evil.example.com/emails")
	if err == nil || !strings.Contains(err.Error(), "not on the configured API origin") {
		t.Errorf("err = %v", err)
	}
}

func TestBuildURLRejectsAnUnparseableBaseURL(t *testing.T) {
	client := testClient(t, WithBaseURL("://nonsense"))

	if _, err := client.buildURL("emails"); err == nil {
		t.Error("expected an error for an unparseable base URL")
	}
}

func TestAPIErrorMessageFallsBackThroughNameThenStatus(t *testing.T) {
	cases := []struct {
		err  APIError
		want string
	}{
		{APIError{Message: "boom", ErrorName: "x", StatusCode: 400}, "boom"},
		{APIError{ErrorName: "quota_exceeded", StatusCode: 402}, "quota_exceeded"},
		{APIError{StatusCode: 418}, "HTTP 418"},
	}
	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}

func TestSentinelForCoversEveryBranch(t *testing.T) {
	if sentinelFor(429) != ErrRateLimit {
		t.Error("429 should map to ErrRateLimit")
	}
	if sentinelFor(502) != ErrServer {
		t.Error("any other 5xx should map to ErrServer")
	}
	if sentinelFor(418) != ErrAPI {
		t.Error("anything else should map to ErrAPI")
	}
}

func TestAPIErrorIsMatchesItsCategoryAndErrAPI(t *testing.T) {
	err := &APIError{StatusCode: 404}

	if !errors.Is(err, ErrNotFound) || !errors.Is(err, ErrAPI) {
		t.Error("a 404 should match both ErrNotFound and ErrAPI")
	}
	if errors.Is(err, ErrRateLimit) {
		t.Error("a 404 must not match ErrRateLimit")
	}
}

func TestDecodeObjectToleratesEmptyAndMalformedBodies(t *testing.T) {
	if got := decodeObject(nil); len(got) != 0 {
		t.Errorf("nil body = %v", got)
	}
	if got := decodeObject([]byte("<html>")); len(got) != 0 {
		t.Errorf("malformed body = %v", got)
	}
	if got := decodeObject([]byte(`{"a":1}`)); got["a"] != float64(1) {
		t.Errorf("valid body = %v", got)
	}
}

func TestNewRequestRejectsAnUnencodableBody(t *testing.T) {
	spec := requestSpec{path: "emails", body: map[string]any{"bad": make(chan int)}}

	if _, err := testClient(t).newRequest(context.Background(), spec); err == nil {
		t.Error("expected an error for a body JSON cannot represent")
	}
}

func TestNewRequestDefaultsTheMethodToPost(t *testing.T) {
	req, err := testClient(t).newRequest(context.Background(), requestSpec{path: "emails"})
	if err != nil {
		t.Fatalf("newRequest: %v", err)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %q", req.Method)
	}
}

func TestDoSurfacesABuildFailureBeforeSending(t *testing.T) {
	_, _, err := testClient(t).do(context.Background(), requestSpec{path: "https://evil.example.com/x"})
	if err == nil || errors.Is(err, ErrConnection) {
		t.Errorf("err = %v, want the origin-guard error", err)
	}
}

func TestVersionIsNeverEmpty(t *testing.T) {
	if Version() == "" {
		t.Error("Version() must never be empty")
	}
}

func TestReleaseVersionStripsTheTagPrefixAndRejectsNonReleases(t *testing.T) {
	cases := []struct {
		path, version, want string
		ok                  bool
	}{
		{modulePath, "v1.4.0", "1.4.0", true},
		{modulePath, "v1.0.0-rc.1", "1.0.0-rc.1", true},
		// A build from this module's own tree records `(devel)`, not a tag. Accepting it would
		// put `mailkube-go/(devel)` on the wire.
		{modulePath, "(devel)", "", false},
		{modulePath, "", "", false},
		{"github.com/other/module", "v1.0.0", "", false},
	}
	for _, tc := range cases {
		got, ok := releaseVersion(tc.path, tc.version)
		if got != tc.want || ok != tc.ok {
			t.Errorf("releaseVersion(%q, %q) = (%q, %v), want (%q, %v)",
				tc.path, tc.version, got, ok, tc.want, tc.ok)
		}
	}
}

// --- logging seams -----------------------------------------------------------------------

func TestResolveLoggerPrefersTheExplicitLogger(t *testing.T) {
	explicit := slog.New(slog.NewTextHandler(io.Discard, nil))

	if got := resolveLogger(explicit); got != explicit {
		t.Error("an explicit logger must win over the environment")
	}
}

func TestResolveLoggerIsSilentByDefault(t *testing.T) {
	t.Setenv(envLogLevel, "")

	logger := resolveLogger(nil)
	if logger.Enabled(context.Background(), slog.LevelError) {
		t.Error("logging must be off unless it is asked for")
	}
}

func TestResolveLoggerHonoursTheEnvironment(t *testing.T) {
	for _, level := range []string{"debug", "INFO", "not-a-level"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv(envLogLevel, level)

			// An unparseable value still turns logging on: the caller asked for output, and
			// silence is the one outcome they could not debug.
			if !resolveLogger(nil).Enabled(context.Background(), slog.LevelError) {
				t.Errorf("MAILKUBE_LOG=%s produced a silent logger", level)
			}
		})
	}
}

func TestTheDiscardHandlerDropsEverything(t *testing.T) {
	handler := discardHandler{}

	if handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled must report false so slog never builds a record")
	}
	if err := handler.Handle(context.Background(), slog.Record{}); err != nil {
		t.Errorf("Handle: %v", err)
	}
	// slog never calls these on a disabled handler, but a caller doing logger.With(...) does.
	if handler.WithAttrs([]slog.Attr{slog.String("a", "b")}) != handler {
		t.Error("WithAttrs must stay a discard handler")
	}
	if handler.WithGroup("group") != handler {
		t.Error("WithGroup must stay a discard handler")
	}
}

func TestRedactHeadersMasksOnlyTheSecrets(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer mk_secret")
	headers.Set("Idempotency-Key", "order-42")
	headers.Set("User-Agent", "mailkube-go/1.0.0")

	safe := redactHeaders(headers)

	if safe["Authorization"] != redactedValue || safe["Idempotency-Key"] != redactedValue {
		t.Errorf("secrets survived redaction: %v", safe)
	}
	if safe["User-Agent"] != "mailkube-go/1.0.0" {
		t.Errorf("a non-secret header was mangled: %v", safe)
	}
}

// --- wire helpers ------------------------------------------------------------------------

func TestItemPathKeepsAnIdentifierInOneSegment(t *testing.T) {
	if got := itemPath("scheduled-emails", "a/b?c"); got != "scheduled-emails/a%2Fb%3Fc" {
		t.Errorf("itemPath = %q", got)
	}
}

func TestRenderTimeIsEmptyForTheZeroInstant(t *testing.T) {
	if got := renderTime(time.Time{}); got != "" {
		t.Errorf("renderTime(zero) = %q, want empty", got)
	}
	want := "2026-08-20T07:00:00Z"
	if got := renderTime(time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC)); got != want {
		t.Errorf("renderTime = %q, want %q", got, want)
	}
}

func TestRefuseRedirectNamesTheTargetAndIsATransportFailure(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://elsewhere.example.com/x", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	err = refuseRedirect(req, nil)
	if !errors.Is(err, ErrConnection) {
		t.Errorf("err = %v, want ErrConnection", err)
	}
	if !strings.Contains(err.Error(), "elsewhere.example.com") {
		t.Errorf("err = %v, want the refused target named", err)
	}
}
