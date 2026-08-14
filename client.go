package mailkube

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the API base URL used when nothing else is configured.
const DefaultBaseURL = "https://api.mailkube.com/mta/v1/"

const (
	envAPIKey  = "MAILKUBE_API_KEY"
	envBaseURL = "MAILKUBE_BASE_URL"
	modulePath = "github.com/mailkube/mailkube-go"
)

// ErrNoAPIKey is returned by New when no API key is supplied or found in the environment.
var ErrNoAPIKey = fmt.Errorf("%w: no API key provided: pass WithAPIKey or set %s", ErrMailkube, envAPIKey)

// Option configures a Client. Go has no keyword arguments, so functional options are this
// SDK's translation of the other SDKs' named parameters.
type Option func(*Client)

// WithAPIKey sets the API key, overriding the environment.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL overrides the API base URL.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithTimeout sets the per-request timeout. Ignored when WithHTTPClient is used.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.timeout = timeout }
}

// WithHTTPClient injects the *http.Client used for every request.
//
// This is the dependency-inversion seam: pass a client configured with your own transport,
// proxy or instrumentation, or a stub in tests. It is what lets the whole suite run without
// network access.
//
// Note that an injected client brings its own redirect policy: the refuse-redirects guard
// described on New applies only to the client this package builds for you.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// Client is the API client, and this package's composition root.
//
// Create one and reuse it; it is safe for concurrent use.
//
//	client, err := mailkube.New()  // reads MAILKUBE_API_KEY
//	email, err := client.Emails.Send(ctx, mailkube.SendEmailParams{
//		From:    "Acme <hello@yourdomain.com>",
//		To:      []string{"customer@example.com"},
//		Subject: "Hello world",
//		HTML:    "<p>It works!</p>",
//	})
//
// There are deliberately no built-in retries. An APIError matching ErrRateLimit carries
// RetryAfter and one matching ErrServer is safe to retry with backoff, so the calling
// application decides. Set SendEmailParams.IdempotencyKey to make a retry safe.
type Client struct {
	// Emails is the emails namespace.
	Emails *EmailsService
	// ScheduledEmails is the scheduled-emails namespace, including its batch operations.
	ScheduledEmails *ScheduledEmailsService

	apiKey     string
	baseURL    string
	timeout    time.Duration
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a client, resolving configuration from the options then the environment.
//
// It returns ErrNoAPIKey when no key is available.
//
// The client it builds refuses HTTP redirects. Go's default policy rewrites a redirected
// PATCH or DELETE into a GET and drops the body, which would turn a redirected cancel into a
// read that answers 200 and reports a cancellation that never happened.
func New(opts ...Option) (*Client, error) {
	c := &Client{timeout: 30 * time.Second}
	for _, opt := range opts {
		opt(c)
	}

	if c.apiKey == "" {
		c.apiKey = os.Getenv(envAPIKey)
	}
	if c.apiKey == "" {
		return nil, ErrNoAPIKey
	}
	if c.baseURL == "" {
		if fromEnv := os.Getenv(envBaseURL); fromEnv != "" {
			c.baseURL = fromEnv
		} else {
			c.baseURL = DefaultBaseURL
		}
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout, CheckRedirect: refuseRedirect}
	}
	c.logger = resolveLogger(c.logger)

	c.Emails = &EmailsService{transport: c}
	c.ScheduledEmails = newScheduledEmailsService(c)
	return c, nil
}

// refuseRedirect is the client's redirect policy: never follow one.
func refuseRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("%w: refusing to follow a redirect to %q", ErrConnection, req.URL)
}

// defaultHeaders returns the auth and non-browser User-Agent headers sent on every request.
//
// Content-Type is deliberately absent: it is set per request, and only when there is a body,
// so a bodyless GET or DELETE does not claim to carry JSON.
func (c *Client) defaultHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + c.apiKey,
		"User-Agent":    "mailkube-go/" + Version(),
		"Accept":        "application/json",
	}
}

// buildURL joins a relative path onto the base URL, refusing any absolute URL off its origin.
//
// Every request carries the Authorization header, so following a link that names a foreign host
// would hand that host the API key. Enforcing it here rather than in a service protects every
// future link-following feature for free. It covers the URLs this SDK builds and the pagination
// links the API issues; a redirect target chosen by the server is refused separately, by the
// client's redirect policy.
func (c *Client) buildURL(path string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL %q: %w", c.baseURL, err)
	}
	resolved, err := base.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", path, err)
	}
	if resolved.Scheme != base.Scheme || resolved.Host != base.Host {
		return "", fmt.Errorf("%w: refusing to follow %q: it is not on the configured API origin",
			ErrMailkube, resolved)
	}
	return resolved.String(), nil
}

// Version returns the module's version as recorded in the consuming build.
//
// There is deliberately no literal here. semantic-release tags the release, the Go toolchain
// records the module version in the build info of anything that depends on this package, and
// this function reads it back, so the reported version equals the released version by
// construction. A hand-maintained constant is how a package ends up reporting a version it is
// not. It reads "0.0.0" when built from the module's own source tree, which is expected.
func Version() string { return version() }

// version memoizes readVersion. defaultHeaders calls Version on every request, and the build
// info it reads is fixed for the life of the process, so resolving it once keeps a linear scan
// of the dependency list off the request path.
var version = sync.OnceValue(readVersion)

// fallbackVersion is reported when the build records no released version for this module.
const fallbackVersion = "0.0.0"

// readVersion scans the build info for this module's recorded version.
func readVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackVersion
	}
	for _, dep := range info.Deps {
		if trimmed, ok := releaseVersion(dep.Path, dep.Version); ok {
			return trimmed
		}
	}
	if trimmed, ok := releaseVersion(info.Main.Path, info.Main.Version); ok {
		return trimmed
	}
	return fallbackVersion
}

// releaseVersion reports the bare version for a build-info entry naming this module.
//
// Two things have to be stripped, and neither is cosmetic. The recorded version is the git tag and
// tagFormat is `v${version}`, so the leading `v` has to go or the User-Agent reads
// `mailkube-go/v1.0.0` instead of the contract's `mailkube-<lang>/<version>`. And a build from the
// module's own tree records the literal `(devel)` rather than a tag, which is not a version at all;
// only a `vX.Y.Z` string is accepted, so anything else falls through to fallbackVersion.
func releaseVersion(path, version string) (string, bool) {
	if path != modulePath || !strings.HasPrefix(version, "v") {
		return "", false
	}
	return strings.TrimPrefix(version, "v"), true
}
