package mailkube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	// defaultMaxBodyBytes caps an inbound delivery. The platform's own outgoing payload limit
	// is far below this, so the cap only ever stops something that is not a real delivery.
	defaultMaxBodyBytes = 1 << 20

	// maxChallengeBytes caps what the registration handshake will echo back. The platform
	// sends 32 hex characters; anything larger is not the handshake.
	maxChallengeBytes = 256

	challengeModeParam  = "hub.mode"
	challengeValueParam = "hub.challenge"
	challengeMode       = "subscribe"
)

// WebhookHandler serves a webhook endpoint: the registration handshake and every delivery.
//
// Mount it anywhere an http.Handler goes, on GET and POST both, because registration probes
// with GET and deliveries arrive as POST:
//
//	h := mailkube.WebhookHandler{Secret: secret, OnEvent: onEvent}
//	http.Handle("/webhooks", h)                                  // net/http, chi
//	r.Match([]string{"GET", "POST"}, "/webhooks", gin.WrapH(h))  // gin
//	e.Any("/webhooks", echo.WrapHandler(h))                      // echo
//	app.All("/webhooks", adaptor.HTTPHandler(h))                 // fiber
//
// Answering the handshake is not optional. Before the platform saves an endpoint it probes the
// URL and requires the hub.challenge value echoed back verbatim; an endpoint that fails the
// probe is never created, and no event is ever delivered to it.
//
// OnEvent runs inside the request, on the request's context, and the sender gives a delivery
// ten seconds. Acknowledge fast and do the work elsewhere: an endpoint whose acceptance rate
// falls below 60% over 24 hours is disabled automatically, so a slow or failing OnEvent
// eventually switches your endpoint off.
type WebhookHandler struct {
	// Secret is the endpoint's signing secret. Required.
	Secret string
	// OnEvent receives each verified delivery. A nil OnEvent verifies and acknowledges without
	// dispatching, which is a useful way to stand an endpoint up before wiring the handling.
	OnEvent func(ctx context.Context, event *Event) error
	// Tolerance is the accepted clock skew on the delivery timestamp. Zero means 300 seconds.
	Tolerance time.Duration
	// MaxBodyBytes caps the request body. Zero means 1 MiB.
	MaxBodyBytes int64
	// OnError, when set, owns the whole error response: nothing else is written for a failed
	// delivery. Use it to log, or to answer something other than the default status.
	OnError func(w http.ResponseWriter, r *http.Request, err error)
}

// ServeHTTP implements http.Handler.
//
// GET is the registration handshake; POST is a delivery. A delivery answers 204 once OnEvent
// returns, 401 for a signature or freshness failure, 413 for an oversized body, 400 for a body
// that is not a webhook, and 500 when OnEvent returns an error.
func (h WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handshake(w, r)
	case http.MethodPost:
		h.deliver(w, r)
	default:
		h.fail(w, r, http.StatusMethodNotAllowed, errors.New("webhook endpoints accept GET and POST"))
	}
}

// handshake answers the endpoint-registration probe by echoing its challenge.
func (h WebhookHandler) handshake(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	challenge := query.Get(challengeValueParam)
	if query.Get(challengeModeParam) != challengeMode || challenge == "" || len(challenge) > maxChallengeBytes {
		h.fail(w, r, http.StatusBadRequest, errors.New("not a webhook registration handshake"))
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, challenge)
}

// deliver verifies one delivery and hands it to OnEvent.
func (h WebhookHandler) deliver(w http.ResponseWriter, r *http.Request) {
	payload, err := h.readBody(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, r, http.StatusRequestEntityTooLarge, err)
			return
		}
		h.fail(w, r, http.StatusBadRequest, err)
		return
	}

	event, err := Verify(payload, r.Header, h.Secret, h.Tolerance)
	if err != nil {
		if errors.Is(err, ErrSignatureVerification) {
			h.fail(w, r, http.StatusUnauthorized, err)
			return
		}
		h.fail(w, r, http.StatusBadRequest, err)
		return
	}

	if h.OnEvent != nil {
		if err := h.OnEvent(r.Context(), event); err != nil {
			h.fail(w, r, http.StatusInternalServerError, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// readBody reads the raw request body under the configured cap.
func (h WebhookHandler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limit := h.MaxBodyBytes
	if limit <= 0 {
		limit = defaultMaxBodyBytes
	}
	return io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
}

// fail writes an error response, or defers the whole response to OnError when one is set.
func (h WebhookHandler) fail(w http.ResponseWriter, r *http.Request, status int, err error) {
	if h.OnError != nil {
		h.OnError(w, r, err)
		return
	}
	http.Error(w, http.StatusText(status), status)
}
