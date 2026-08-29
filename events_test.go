package mailkube_test

import (
	"errors"
	"strings"
	"testing"

	mailkube "github.com/mailkube/mailkube-go"
)

// messageContext is the block every email.* fixture opens with.
const messageContext = `"email_id":"em_1","created_at":"2026-08-13T09:00:00Z","domain":"acme.com",
	"subject":"Hi","to":["a@b.com"],"from":"hello@acme.com","tags":[{"name":"campaign","value":"spring"}]`

// eventFixtures is one payload per modelled event type.
//
// They live here rather than under testdata/ because .jscpd.json excludes *_test.go by path but
// not testdata/, and eleven near-identical JSON documents would otherwise eat the duplication
// budget. The map is keyed by event type so the catalogue guard below can match it against
// mailkube.EventTypes() in both directions.
var eventFixtures = map[string]string{
	mailkube.EventTypeEmailSent: `{"type":"email.sent","created_at":"2026-08-13T09:00:01Z","data":{` +
		messageContext + `,"sent":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:01Z"}}}`,
	mailkube.EventTypeEmailDelivered: `{"type":"email.delivered","created_at":"2026-08-13T09:00:02Z","data":{` +
		messageContext + `,"delivery":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:02Z"}}}`,
	mailkube.EventTypeEmailBounced: `{"type":"email.bounced","created_at":"2026-08-13T09:00:03Z","data":{` +
		messageContext + `,"bounce":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:03Z",
		"code":550,"reason":"mailbox unavailable"}}}`,
	mailkube.EventTypeEmailDeliveryDelayed: `{"type":"email.delivery_delayed",
		"created_at":"2026-08-13T09:00:04Z","data":{` + messageContext + `,
		"delay":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:04Z","code":451,"reason":"greylisted"}}}`,
	mailkube.EventTypeEmailSuppressed: `{"type":"email.suppressed","created_at":"2026-08-13T09:00:05Z","data":{` +
		messageContext + `,"suppression":{"recipients":["a@b.com"],"timestamp":"2026-08-13T09:00:05Z"}}}`,
	mailkube.EventTypeEmailScheduled: `{"type":"email.scheduled","created_at":"2026-08-13T09:00:06Z","data":{` +
		messageContext + `,"scheduled":{"scheduled_at":"2026-08-20T07:00:00Z","batch_id":"launch"}}}`,
	mailkube.EventTypeEmailFailed: `{"type":"email.failed","created_at":"2026-08-13T09:00:07Z","data":{` +
		messageContext + `,"failed":{"reason":"suppressed_at_dispatch","timestamp":"2026-08-13T09:00:07Z"}}}`,
	mailkube.EventTypeEmailOpened: `{"type":"email.opened","created_at":"2026-08-13T09:00:08Z","data":{` +
		messageContext + `,"open":{"ipAddress":"203.0.113.7","userAgent":"Mozilla/5.0",
		"timestamp":"2026-08-13T09:00:08Z"}}}`,
	mailkube.EventTypeEmailClicked: `{"type":"email.clicked","created_at":"2026-08-13T09:00:09Z","data":{` +
		messageContext + `,"click":{"ipAddress":"203.0.113.7","userAgent":"Mozilla/5.0",
		"timestamp":"2026-08-13T09:00:09Z","link":"https://acme.com/pricing"}}}`,
	mailkube.EventTypeDomainStatus: `{"type":"domain.status","created_at":"2026-08-13T09:00:10Z",
		"data":{"domain":"acme.com","status":"verified","onboarding_state":"complete",
		"previous":{"status":"pending","onboarding_state":"dns_published"}}}`,
	mailkube.EventTypeWebhookStatus: `{"type":"webhook.status","created_at":"2026-08-13T09:00:11Z",
		"data":{"endpoint_url":"https://acme.com/hooks","is_active":false,"is_deleted":false,
		"disabled_reason":"quality_threshold","previous":{"is_active":true,"is_deleted":false,
		"disabled_reason":""}}}`,
}

// eventChecks asserts, per event type, that the payload landed in its own type with its own
// distinctive nested block populated.
//
// The distinctive-field assertion is what catches a mis-keyed registration: registering
// &SentData{} under "email.delivered" type-checks and would satisfy any assertion weaker than
// this one, because the unknown "delivery" key is simply ignored and the struct comes back
// empty.
var eventChecks = map[string]func(*testing.T, *mailkube.Event){
	mailkube.EventTypeEmailSent: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.SentData)
		if !ok {
			t.Fatalf("data is %T, want *SentData", event.Data)
		}
		if data.Sent.Recipient != "a@b.com" {
			t.Errorf("sent = %+v", data.Sent)
		}
	},
	mailkube.EventTypeEmailDelivered: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.DeliveredData)
		if !ok {
			t.Fatalf("data is %T, want *DeliveredData", event.Data)
		}
		if data.Delivery.Recipient != "a@b.com" {
			t.Errorf("delivery = %+v", data.Delivery)
		}
	},
	mailkube.EventTypeEmailBounced: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.BouncedData)
		if !ok {
			t.Fatalf("data is %T, want *BouncedData", event.Data)
		}
		if data.Bounce.Code != 550 || data.Bounce.Reason != "mailbox unavailable" {
			t.Errorf("bounce = %+v", data.Bounce)
		}
		if data.Bounce.Recipient != "a@b.com" {
			t.Errorf("the embedded delivery context did not flatten: %+v", data.Bounce)
		}
	},
	mailkube.EventTypeEmailDeliveryDelayed: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.DelayedData)
		if !ok {
			t.Fatalf("data is %T, want *DelayedData", event.Data)
		}
		if data.Delay.Code != 451 {
			t.Errorf("delay = %+v", data.Delay)
		}
	},
	mailkube.EventTypeEmailSuppressed: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.SuppressedData)
		if !ok {
			t.Fatalf("data is %T, want *SuppressedData", event.Data)
		}
		if len(data.Suppression.Recipients) != 1 {
			t.Errorf("suppression = %+v", data.Suppression)
		}
	},
	mailkube.EventTypeEmailScheduled: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.ScheduledData)
		if !ok {
			t.Fatalf("data is %T, want *ScheduledData", event.Data)
		}
		if data.Scheduled.BatchID == nil || *data.Scheduled.BatchID != "launch" {
			t.Errorf("scheduled = %+v", data.Scheduled)
		}
	},
	mailkube.EventTypeEmailFailed: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.FailedData)
		if !ok {
			t.Fatalf("data is %T, want *FailedData", event.Data)
		}
		if data.Failed.Reason != "suppressed_at_dispatch" {
			t.Errorf("failed = %+v", data.Failed)
		}
	},
	mailkube.EventTypeEmailOpened: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.OpenedData)
		if !ok {
			t.Fatalf("data is %T, want *OpenedData", event.Data)
		}
		if data.Open.IPAddress != "203.0.113.7" || data.Open.UserAgent != "Mozilla/5.0" {
			t.Errorf("open = %+v: the camelCase wire keys did not bind", data.Open)
		}
	},
	mailkube.EventTypeEmailClicked: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.ClickedData)
		if !ok {
			t.Fatalf("data is %T, want *ClickedData", event.Data)
		}
		if data.Click.Link != "https://acme.com/pricing" || data.Click.IPAddress != "203.0.113.7" {
			t.Errorf("click = %+v", data.Click)
		}
	},
	mailkube.EventTypeDomainStatus: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.DomainStatusData)
		if !ok {
			t.Fatalf("data is %T, want *DomainStatusData", event.Data)
		}
		if data.Status != "verified" || data.Previous.Status != "pending" {
			t.Errorf("domain status = %+v", data)
		}
	},
	mailkube.EventTypeWebhookStatus: func(t *testing.T, event *mailkube.Event) {
		data, ok := event.Data.(*mailkube.WebhookStatusData)
		if !ok {
			t.Fatalf("data is %T, want *WebhookStatusData", event.Data)
		}
		if data.DisabledReason != "quality_threshold" || !data.Previous.IsActive {
			t.Errorf("webhook status = %+v", data)
		}
	},
}

// TestTheCatalogueTheFixturesAndTheChecksAllAgree is the guard that an event type was wired up
// completely. mailkube.EventTypes() is derived from the registry, so a type registered without a
// fixture fails here, and a fixture for a type nobody registered fails here too.
func TestTheCatalogueTheFixturesAndTheChecksAllAgree(t *testing.T) {
	// The README's event table documents the same set. Update all four together.
	const documentedEventTypes = 11

	catalogue := mailkube.EventTypes()
	if len(catalogue) != documentedEventTypes {
		t.Errorf("the catalogue has %d types, the README documents %d", len(catalogue), documentedEventTypes)
	}
	if len(eventFixtures) != len(catalogue) || len(eventChecks) != len(catalogue) {
		t.Errorf("catalogue %d, fixtures %d, checks %d: all three must cover the same types",
			len(catalogue), len(eventFixtures), len(eventChecks))
	}
	for _, eventType := range catalogue {
		if _, ok := eventFixtures[eventType]; !ok {
			t.Errorf("%s is registered but has no fixture", eventType)
		}
		if _, ok := eventChecks[eventType]; !ok {
			t.Errorf("%s is registered but has no field assertion", eventType)
		}
	}
}

func TestEveryModelledEventParsesIntoItsOwnType(t *testing.T) {
	for _, eventType := range mailkube.EventTypes() {
		t.Run(eventType, func(t *testing.T) {
			event, err := mailkube.ParseEvent([]byte(eventFixtures[eventType]))
			if err != nil {
				t.Fatalf("ParseEvent: %v", err)
			}
			if event.Type != eventType {
				t.Errorf("type = %q, want %q", event.Type, eventType)
			}
			if event.CreatedAt == "" {
				t.Error("created_at did not bind")
			}
			eventChecks[eventType](t, event)
		})
	}
}

func TestTheMessageContextFlattensAndCarriesTags(t *testing.T) {
	event, err := mailkube.ParseEvent([]byte(eventFixtures[mailkube.EventTypeEmailBounced]))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}

	data := event.Data.(*mailkube.BouncedData)
	if data.EmailID != "em_1" {
		t.Errorf("email_id = %q", data.EmailID)
	}
	if data.Domain == nil || *data.Domain != "acme.com" || data.From == nil || *data.From != "hello@acme.com" {
		t.Errorf("domain/from did not bind: %+v", data.MessageContext)
	}
	if len(data.Tags) != 1 || data.Tags[0].Name != "campaign" {
		t.Errorf("tags = %+v", data.Tags)
	}
}

func TestAResolvableFieldSentAsNullStaysDistinctFromEmpty(t *testing.T) {
	payload := `{"type":"email.sent","created_at":"2026-08-13T09:00:01Z","data":{"email_id":"em_1",
		"created_at":"2026-08-13T09:00:01Z","domain":null,"subject":null,"to":null,"from":null,
		"sent":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:01Z"}}}`

	event, err := mailkube.ParseEvent([]byte(payload))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}

	data := event.Data.(*mailkube.SentData)
	if data.Domain != nil || data.Subject != nil || data.From != nil || data.To != nil {
		t.Errorf("a null must stay nil, not collapse to an empty value: %+v", data.MessageContext)
	}
}

func TestAnUnknownEventTypeIsAValidParseResult(t *testing.T) {
	payload := `{"type":"email.teleported","created_at":"2026-08-13T09:00:00Z",
		"data":{"email_id":"em_1","destination":"mars"}}`

	event, err := mailkube.ParseEvent([]byte(payload))
	if err != nil {
		t.Fatalf("an unknown event type must not be an error: %v", err)
	}

	data, ok := event.Data.(*mailkube.UnknownData)
	if !ok {
		t.Fatalf("data is %T, want *UnknownData", event.Data)
	}
	if event.Type != "email.teleported" {
		t.Errorf("type = %q", event.Type)
	}
	if data.Fields["destination"] != "mars" {
		t.Errorf("fields = %v", data.Fields)
	}
}

func TestAModelledEventWithADriftedFieldDegradesInsteadOfFailing(t *testing.T) {
	// bounce.code as a string is the shape a strict decoder rejects and pydantic coerces. A
	// released receiver must not start answering 400 because one field changed shape.
	payload := `{"type":"email.bounced","created_at":"2026-08-13T09:00:03Z","data":{"email_id":"em_1",
		"bounce":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:03Z","code":"550","reason":"nope"}}}`

	event, err := mailkube.ParseEvent([]byte(payload))
	if err != nil {
		t.Fatalf("a drifted field must not be an error: %v", err)
	}

	data, ok := event.Data.(*mailkube.UnknownData)
	if !ok {
		t.Fatalf("data is %T, want *UnknownData", event.Data)
	}
	if data.Fields["email_id"] != "em_1" {
		t.Errorf("the payload must still be readable: %v", data.Fields)
	}
}

func TestRawKeepsFieldsThisVersionPredates(t *testing.T) {
	payload := `{"type":"email.sent","created_at":"2026-08-13T09:00:01Z","future_top_level":1,
		"data":{"email_id":"em_1","sent":{"recipient":"a@b.com","timestamp":"2026-08-13T09:00:01Z",
		"future_nested":"kept"}}}`

	event, err := mailkube.ParseEvent([]byte(payload))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}

	raw := string(event.Raw)
	if !strings.Contains(raw, "future_top_level") || !strings.Contains(raw, "future_nested") {
		t.Errorf("Raw dropped a field: %s", raw)
	}
}

func TestABodyThatIsNotJSONIsAnError(t *testing.T) {
	_, err := mailkube.ParseEvent([]byte("not json"))
	if !errors.Is(err, mailkube.ErrUnexpectedResponse) {
		t.Errorf("err = %v, want ErrUnexpectedResponse", err)
	}
}

func TestParseEventLeavesTheDeliveryHeadersEmpty(t *testing.T) {
	event, err := mailkube.ParseEvent([]byte(eventFixtures[mailkube.EventTypeEmailSent]))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if event.ID != "" || event.Timestamp != "" {
		t.Error("ParseEvent has no headers to read: ID and Timestamp must stay empty")
	}
}

// TestEngagementWithoutIPOrUserAgent proves a released client survives the payload a current
// server sends. The platform stopped recording the recipient's address and client, so both keys
// are absent from the wire and must decode to the zero value rather than failing the parse.
func TestEngagementWithoutIPOrUserAgent(t *testing.T) {
	body := `{"type":"email.opened","created_at":"2026-08-13T09:00:08Z","data":{` +
		messageContext + `,"open":{"timestamp":"2026-08-13T09:00:08Z"}}}`

	event, err := mailkube.ParseEvent([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	data, ok := event.Data.(*mailkube.OpenedData)
	if !ok {
		t.Fatalf("data is %T, want *OpenedData", event.Data)
	}
	if data.Open.IPAddress != "" {
		t.Errorf("IPAddress = %q, want empty", data.Open.IPAddress)
	}
	if data.Open.UserAgent != "" {
		t.Errorf("UserAgent = %q, want empty", data.Open.UserAgent)
	}
	if data.Open.Timestamp != "2026-08-13T09:00:08Z" {
		t.Errorf("Timestamp = %q", data.Open.Timestamp)
	}
}
