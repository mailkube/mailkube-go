package mailkube

import (
	"encoding/json"
	"fmt"
	"sort"
)

// The event types this SDK version models. Anything else parses as UnknownData, so a new
// server-side event never breaks a receiver built against an older SDK.
const (
	EventTypeEmailSent            = "email.sent"
	EventTypeEmailDelivered       = "email.delivered"
	EventTypeEmailBounced         = "email.bounced"
	EventTypeEmailDeliveryDelayed = "email.delivery_delayed"
	EventTypeEmailSuppressed      = "email.suppressed"
	EventTypeEmailScheduled       = "email.scheduled"
	EventTypeEmailFailed          = "email.failed"
	EventTypeEmailOpened          = "email.opened"
	EventTypeEmailClicked         = "email.clicked"
	EventTypeDomainStatus         = "domain.status"
	EventTypeWebhookStatus        = "webhook.status"
)

// eventTypes is the catalogue: it maps an event type to a constructor for its data payload.
//
// Go has no discriminated union, so this map plays the part Python's union plays: it is the one
// place an event type is registered, and both ParseEvent and the catalogue test read it rather
// than a second hand-maintained list. Registering a type is one line here.
var eventTypes = map[string]func() any{
	EventTypeEmailSent:            func() any { return &SentData{} },
	EventTypeEmailDelivered:       func() any { return &DeliveredData{} },
	EventTypeEmailBounced:         func() any { return &BouncedData{} },
	EventTypeEmailDeliveryDelayed: func() any { return &DelayedData{} },
	EventTypeEmailSuppressed:      func() any { return &SuppressedData{} },
	EventTypeEmailScheduled:       func() any { return &ScheduledData{} },
	EventTypeEmailFailed:          func() any { return &FailedData{} },
	EventTypeEmailOpened:          func() any { return &OpenedData{} },
	EventTypeEmailClicked:         func() any { return &ClickedData{} },
	EventTypeDomainStatus:         func() any { return &DomainStatusData{} },
	EventTypeWebhookStatus:        func() any { return &WebhookStatusData{} },
}

// EventTypes returns every event type this SDK version models, sorted.
//
// Derived from the catalogue rather than written out a second time, so it cannot drift: a type
// that is registered is listed, and a type that is listed is registered. Tests use it as the
// anchor that a new event was wired up completely, and a receiver can use it to tell a modelled
// event from one it will see as *UnknownData.
func EventTypes() []string {
	types := make([]string, 0, len(eventTypes))
	for eventType := range eventTypes {
		types = append(types, eventType)
	}
	sort.Strings(types)
	return types
}

// Event is one inbound webhook delivery.
//
// Data holds the payload typed for this event, for example *BouncedData, and *UnknownData for
// anything this SDK version does not model. Switch on it:
//
//	switch data := event.Data.(type) {
//	case *mailkube.BouncedData:
//		log.Println(data.EmailID, data.Bounce.Reason)
//	case *mailkube.UnknownData:
//		log.Println("new event type", event.Type, data.Fields)
//	}
type Event struct {
	// Type is the event type, e.g. email.bounced.
	Type string
	// CreatedAt is when the event was emitted, as the verbatim string the server sent.
	CreatedAt string
	// ID is the delivery id from X-Webhook-Id. It is stable across retries, so it is the key
	// to deduplicate on. Populated by Verify and by WebhookHandler; empty from ParseEvent.
	ID string
	// Timestamp is the signed delivery timestamp from X-Webhook-Ts, populated with ID.
	Timestamp string
	// Data is the typed payload: one of the *Data types, or *UnknownData.
	Data any
	// Raw is the verbatim request body.
	//
	// Typed structs drop fields this SDK version predates, so a receiver that logs or forwards
	// an event forwards Raw, which keeps every field at every nesting depth.
	Raw json.RawMessage
}

// MessageContext is the block every email.* event's data carries.
//
// Domain, Subject, To and From are always sent as keys but may be null: the server resolves
// them through the sending transaction, which a per-recipient event can briefly outlive. They
// are pointers so that "the server could not resolve this" stays distinguishable from "the
// value is empty".
type MessageContext struct {
	EmailID   string   `json:"email_id"`
	CreatedAt string   `json:"created_at"`
	Domain    *string  `json:"domain"`
	Subject   *string  `json:"subject"`
	To        []string `json:"to"`
	From      *string  `json:"from"`
	Tags      []Tag    `json:"tags"`
}

// DeliveryContext is a single-recipient delivery outcome.
type DeliveryContext struct {
	Recipient string `json:"recipient"`
	Timestamp string `json:"timestamp"`
}

// FailureContext is a delivery failure with the receiving server's code and reason.
type FailureContext struct {
	DeliveryContext
	Code   int    `json:"code"`
	Reason string `json:"reason"`
}

// EngagementContext is an open interaction. Its nested keys are camelCase on the wire.
//
// Deprecated fields: IPAddress and UserAgent are no longer recorded by the platform, so a current
// server omits both keys and they decode to the zero value. They are kept rather than removed so
// that code written against an earlier version still compiles, and so an event replayed from an
// archive still decodes.
type EngagementContext struct {
	IPAddress string `json:"ipAddress"`
	UserAgent string `json:"userAgent"`
	Timestamp string `json:"timestamp"`
}

// ClickContext is a click interaction: an open plus the link that was followed.
type ClickContext struct {
	EngagementContext
	Link string `json:"link"`
}

// SuppressionContext is the set of recipients dropped before sending.
type SuppressionContext struct {
	Recipients []string `json:"recipients"`
	Timestamp  string   `json:"timestamp"`
}

// ScheduledContext is a send accepted for later transmission. BatchID is null when the send was
// not grouped into a batch.
type ScheduledContext struct {
	ScheduledAt string  `json:"scheduled_at"`
	BatchID     *string `json:"batch_id"`
}

// SendFailureContext is an accepted send dropped at dispatch time, before transmission.
//
// Distinct from FailureContext, which reports what a receiving mail server said about one
// recipient. This is message-level and carries no recipient: the send never left. Reason is a
// stable server-side code and stays a plain string, so a newly added reason never breaks
// parsing.
type SendFailureContext struct {
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
}

// DomainStatusPrevious is the prior domain state in a domain.status change.
type DomainStatusPrevious struct {
	Status          string `json:"status"`
	OnboardingState string `json:"onboarding_state"`
}

// WebhookStatusPrevious is the prior endpoint state in a webhook.status change.
type WebhookStatusPrevious struct {
	IsActive       bool   `json:"is_active"`
	IsDeleted      bool   `json:"is_deleted"`
	DisabledReason string `json:"disabled_reason"`
}

// SentData is the data of an email.sent event: accepted and spooled for transmission.
type SentData struct {
	MessageContext
	Sent DeliveryContext `json:"sent"`
}

// DeliveredData is the data of an email.delivered event: accepted by the receiving server.
type DeliveredData struct {
	MessageContext
	Delivery DeliveryContext `json:"delivery"`
}

// BouncedData is the data of an email.bounced event: a permanent failure.
type BouncedData struct {
	MessageContext
	Bounce FailureContext `json:"bounce"`
}

// DelayedData is the data of an email.delivery_delayed event: a transient failure.
type DelayedData struct {
	MessageContext
	Delay FailureContext `json:"delay"`
}

// SuppressedData is the data of an email.suppressed event: recipients dropped before sending.
type SuppressedData struct {
	MessageContext
	Suppression SuppressionContext `json:"suppression"`
}

// ScheduledData is the data of an email.scheduled event: accepted for later transmission.
type ScheduledData struct {
	MessageContext
	Scheduled ScheduledContext `json:"scheduled"`
}

// FailedData is the data of an email.failed event: dropped at dispatch, never transmitted.
type FailedData struct {
	MessageContext
	Failed SendFailureContext `json:"failed"`
}

// OpenedData is the data of an email.opened event: the tracking pixel loaded.
type OpenedData struct {
	MessageContext
	Open EngagementContext `json:"open"`
}

// ClickedData is the data of an email.clicked event: a tracked link was followed.
type ClickedData struct {
	MessageContext
	Click ClickContext `json:"click"`
}

// DomainStatusData is the data of a domain.status event. It carries no message context.
type DomainStatusData struct {
	Domain          string               `json:"domain"`
	Status          string               `json:"status"`
	OnboardingState string               `json:"onboarding_state"`
	Previous        DomainStatusPrevious `json:"previous"`
}

// WebhookStatusData is the data of a webhook.status event. It carries no message context.
type WebhookStatusData struct {
	EndpointURL    string                `json:"endpoint_url"`
	IsActive       bool                  `json:"is_active"`
	IsDeleted      bool                  `json:"is_deleted"`
	DisabledReason string                `json:"disabled_reason"`
	Previous       WebhookStatusPrevious `json:"previous"`
}

// UnknownData is the payload of an event this SDK version does not model, or of a known event
// whose data did not fit its type.
//
// A struct rather than a bare map, so Fields reads the same through a pointer as every other
// payload's fields do. Event.Type and Event.Raw are still populated, so a receiver keeps
// working when the platform introduces an event type or changes a field's shape.
type UnknownData struct {
	// Fields is the event's data block, decoded as generic JSON.
	Fields map[string]any
}

// ParseEvent parses a raw webhook body into a typed Event.
//
// It does not verify anything: use Verify, or VerifySignature followed by this. An unknown
// event type, and a known type whose data does not fit, both degrade to *UnknownData rather
// than erroring, so neither a new event nor a server-side field change can break a receiver
// that is already deployed. Only a body that is not JSON at all is an error.
func ParseEvent(payload []byte) (*Event, error) {
	var envelope struct {
		Type      string          `json:"type"`
		CreatedAt string          `json:"created_at"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("%w: could not decode the webhook body: %w", ErrUnexpectedResponse, err)
	}

	return &Event{
		Type:      envelope.Type,
		CreatedAt: envelope.CreatedAt,
		Data:      decodeEventData(envelope.Type, envelope.Data),
		Raw:       append(json.RawMessage(nil), payload...),
	}, nil
}

// decodeEventData decodes an event's data block into its registered type, degrading to
// UnknownData for an unregistered type or a payload that does not fit.
func decodeEventData(eventType string, raw json.RawMessage) any {
	if newData, registered := eventTypes[eventType]; registered {
		data := newData()
		if err := json.Unmarshal(raw, data); err == nil {
			return data
		}
	}

	unknown := &UnknownData{}
	_ = json.Unmarshal(raw, &unknown.Fields)
	return unknown
}
