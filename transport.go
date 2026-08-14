package mailkube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes caps how much of a response body is read into memory. The largest documented
// response on this API is a page of scheduled emails, orders of magnitude below this.
const maxResponseBytes = 8 << 20

// headerRequestID is the response header carrying the server's request id.
const headerRequestID = "X-Request-Id"

// requestSpec is a fully-built request, ready to send.
type requestSpec struct {
	// path is relative to the base URL, or an absolute URL the API itself issued.
	path string
	// method is the HTTP method. Empty means POST.
	method string
	// body is marshaled as the JSON request body, or nil for a body-less request.
	//
	// Builders assign a struct value, never a pointer: an interface holding a typed nil is not
	// nil, so a nil pointer here would be marshaled and sent as the literal `null`.
	body any
	// query is the query string. Builders omit zero-valued filters rather than sending them.
	query url.Values
	// headers are per-request headers, merged over the client defaults.
	headers map[string]string
}

// sendTransport is the narrow interface EmailsService depends on.
//
// There is deliberately one interface per capability rather than one wide one: a service that
// only sends must not acquire a dependency on every other verb. A new capability adds an
// interface; it never widens an existing one. Keeping it unexported means the seam exists for
// testing and layering without becoming public API.
type sendTransport interface {
	sendEmail(ctx context.Context, spec requestSpec) (*Email, error)
}

// jsonTransport is the interface every typed verb depends on: perform a request, hand back the
// raw 2xx body. Parsing belongs to fetch, so the transport stays ignorant of response models.
type jsonTransport interface {
	do(ctx context.Context, spec requestSpec) ([]byte, http.Header, error)
}

// fetch performs a request and decodes the 2xx body into T.
//
// A package-level function rather than a method because Go methods cannot have type parameters.
// An empty, non-object or malformed 2xx body is ErrUnexpectedResponse, never a zero-valued
// model: handing a caller a *ScheduledEmail with no id and calling it success is the one failure
// mode a typed client must not have.
func fetch[T any](ctx context.Context, transport jsonTransport, spec requestSpec) (*T, error) {
	raw, _, err := transport.do(ctx, spec)
	if err != nil {
		return nil, err
	}

	if !isJSONObject(raw) {
		return nil, fmt.Errorf("%w: expected a JSON object body", ErrUnexpectedResponse)
	}

	model := new(T)
	if err := json.Unmarshal(raw, model); err != nil {
		return nil, fmt.Errorf("%w: could not decode the response body: %w", ErrUnexpectedResponse, err)
	}
	return model, nil
}

// isJSONObject reports whether raw is a JSON object, without decoding it.
//
// json.Unmarshal alone is not enough of a guard: decoding the literal `null` into a struct leaves
// the struct untouched and returns no error, so a 2xx body of `null` would hand the caller a
// zero-valued model and call it success — the exact failure fetch exists to prevent. Every verb on
// this API answers with an object, so anything else is a response the SDK cannot read.
func isJSONObject(raw []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(raw, " \t\r\n"), []byte("{"))
}

// sendEmail performs the request and builds the accepted-send result.
//
// It does not go through fetch because one field, IdempotentReplayed, comes from a response
// header rather than the body.
func (c *Client) sendEmail(ctx context.Context, spec requestSpec) (*Email, error) {
	raw, header, err := c.do(ctx, spec)
	if err != nil {
		return nil, err
	}

	email := &Email{}
	if err := json.Unmarshal(raw, email); err != nil {
		return nil, fmt.Errorf("%w: could not decode the response body: %w", ErrUnexpectedResponse, err)
	}
	if email.ID == "" {
		return nil, fmt.Errorf("%w: expected a JSON body with an 'id'", ErrUnexpectedResponse)
	}
	email.IdempotentReplayed = strings.EqualFold(header.Get("Idempotent-Replayed"), "true")
	return email, nil
}

// do performs one HTTP round trip and returns the raw 2xx body.
//
// This is the single place a non-2xx status becomes an error, so every verb, present and
// future, reports failures identically.
func (c *Client) do(ctx context.Context, spec requestSpec) ([]byte, http.Header, error) {
	req, err := c.newRequest(ctx, spec)
	if err != nil {
		return nil, nil, err
	}

	started := time.Now()
	c.logger.DebugContext(ctx, "mailkube request",
		"method", req.Method, "url", req.URL.String(), "headers", redactHeaders(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrConnection, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrConnection, err)
	}

	c.logger.DebugContext(ctx, "mailkube response",
		"status", resp.StatusCode, "request_id", resp.Header.Get(headerRequestID),
		"duration", time.Since(started))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, apiErrorFrom(resp, decodeObject(raw))
	}
	return raw, resp.Header, nil
}

// newRequest builds the *http.Request for a spec.
func (c *Client) newRequest(ctx context.Context, spec requestSpec) (*http.Request, error) {
	endpoint, err := c.buildURL(spec.path)
	if err != nil {
		return nil, err
	}
	// Only specs this SDK builds carry a query, and those always have a query-less path; a
	// pagination link arrives with its query already in place.
	if len(spec.query) > 0 {
		endpoint += "?" + spec.query.Encode()
	}

	var reader io.Reader
	if spec.body != nil {
		encoded, err := json.Marshal(spec.body)
		if err != nil {
			return nil, fmt.Errorf("could not encode the request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	method := spec.method
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("could not build the request: %w", err)
	}
	for name, value := range c.defaultHeaders() {
		req.Header.Set(name, value)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range spec.headers {
		req.Header.Set(name, value)
	}
	return req, nil
}

// apiErrorFrom builds the APIError for a non-2xx response.
func apiErrorFrom(resp *http.Response, payload map[string]any) *APIError {
	name, _ := payload["name"].(string)
	message, _ := payload["message"].(string)
	retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))

	return &APIError{
		ErrorName:  name,
		Message:    message,
		StatusCode: resp.StatusCode,
		Body:       payload,
		RetryAfter: retryAfter,
		RequestID:  resp.Header.Get(headerRequestID),
	}
}

// decodeObject is a best-effort JSON decode; it returns an empty map for an empty or
// undecodable body, so a malformed error response still maps by status.
func decodeObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}
