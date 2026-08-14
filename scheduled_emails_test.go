package mailkube_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	mailkube "github.com/mailkube/mailkube-go"
)

// stubReply is one canned HTTP response.
type stubReply struct {
	status  int
	payload string
}

// stubCall is one request the client made.
type stubCall struct {
	method string
	path   string
	query  string
	body   map[string]any
}

// stubSequence answers each call with the next reply, recording what was asked for. It is the
// scheduled-email counterpart of client_test.go's stubClient, which serves a single reply.
type stubSequence struct {
	replies []stubReply
	calls   []stubCall
}

// client builds a client whose transport is this sequence.
func (s *stubSequence) client(t *testing.T) *mailkube.Client {
	t.Helper()
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// EscapedPath, not Path: Path is already decoded, so it cannot show whether an
		// identifier stayed inside its own segment on the wire.
		call := stubCall{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.RawQuery}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &call.body)
		}
		s.calls = append(s.calls, call)

		reply := stubReply{status: http.StatusOK, payload: "{}"}
		if len(s.calls) <= len(s.replies) {
			reply = s.replies[len(s.calls)-1]
		}
		return &http.Response{
			StatusCode: reply.status,
			Body:       io.NopCloser(strings.NewReader(reply.payload)),
			Header:     http.Header{},
		}, nil
	})}

	client, err := mailkube.New(mailkube.WithAPIKey("mk_test"), mailkube.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// only returns the single call the sequence recorded, failing when there was not exactly one.
func (s *stubSequence) only(t *testing.T) stubCall {
	t.Helper()
	if len(s.calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(s.calls))
	}
	return s.calls[0]
}

const scheduledEmailPayload = `{"id":"se_1","object":"scheduled_email","status":"scheduled",
	"message_id":"<a@b>","scheduled_at":"2026-08-20T07:00:00Z","created_at":"2026-08-13T09:00:00Z",
	"batch_id":"launch","subject":"Hi","recipients":"a@b.com +2","topic":"news",
	"tags":[{"name":"campaign","value":"spring"}]}`

func TestListSendsAGetWithNoQueryForZeroValuedParams(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: `{"data":[],"pagination":{}}`}}}
	client := stub.client(t)

	if _, err := client.ScheduledEmails.List(context.Background(), mailkube.ScheduledEmailListParams{}); err != nil {
		t.Fatalf("List: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodGet {
		t.Errorf("method = %q, want GET", call.method)
	}
	if call.path != "/mta/v1/scheduled-emails" {
		t.Errorf("path = %q", call.path)
	}
	// page=0 would be a hard 422: the server requires page >= 1.
	if call.query != "" {
		t.Errorf("query = %q, want empty", call.query)
	}
}

func TestListRendersEveryFilter(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: `{"data":[],"pagination":{}}`}}}
	client := stub.client(t)

	_, err := client.ScheduledEmails.List(context.Background(), mailkube.ScheduledEmailListParams{
		Status:         []string{"scheduled", "canceled"},
		BatchID:        "launch",
		ScheduledAtGTE: time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC),
		ScheduledAtLTE: time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC),
		Page:           2,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	query := stub.only(t).query
	for _, want := range []string{
		"status=scheduled%2Ccanceled",
		"batch_id=launch",
		"scheduled_at_gte=2026-08-20T07%3A00%3A00Z",
		"scheduled_at_lte=2026-08-21T07%3A00%3A00Z",
		"page=2",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q is missing %q", query, want)
		}
	}
}

func TestListParsesThePageAndItsPagination(t *testing.T) {
	payload := `{"pagination":{"steps":{"next":"https://api.mailkube.com/mta/v1/scheduled-emails?page=2"},
		"total_count":3,"current_page":1},"data":[` + scheduledEmailPayload + `]}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: payload}}}

	page, err := stub.client(t).ScheduledEmails.List(context.Background(), mailkube.ScheduledEmailListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if !page.HasMore() {
		t.Error("HasMore() = false, want true")
	}
	if page.Pagination.TotalCount != 3 || page.Pagination.CurrentPage != 1 {
		t.Errorf("pagination = %+v", page.Pagination)
	}
	if len(page.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(page.Data))
	}
	item := page.Data[0]
	if item.ID != "se_1" || item.Recipients != "a@b.com +2" || item.Topic != "news" {
		t.Errorf("item = %+v", item)
	}
	if len(item.Tags) != 1 || item.Tags[0].Name != "campaign" || item.Tags[0].Value != "spring" {
		t.Errorf("tags = %+v", item.Tags)
	}
}

func TestAllFollowsTheServersNextLinkAcrossPages(t *testing.T) {
	first := `{"pagination":{"steps":{"next":"https://api.mailkube.com/mta/v1/scheduled-emails?page=2"}},
		"data":[{"id":"se_1"}]}`
	second := `{"pagination":{"steps":{}},"data":[{"id":"se_2"}]}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: first}, {status: 200, payload: second}}}
	client := stub.client(t)

	var seen []string
	for item, err := range client.ScheduledEmails.All(context.Background(), mailkube.ScheduledEmailListParams{}) {
		if err != nil {
			t.Fatalf("All: %v", err)
		}
		seen = append(seen, item.ID)
	}

	if strings.Join(seen, ",") != "se_1,se_2" {
		t.Errorf("ids = %v, want [se_1 se_2]", seen)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("made %d calls, want 2", len(stub.calls))
	}
	if stub.calls[1].query != "page=2" {
		t.Errorf("second call query = %q, want page=2", stub.calls[1].query)
	}
}

func TestAllStopsFetchingWhenTheCallerBreaks(t *testing.T) {
	first := `{"pagination":{"steps":{"next":"https://api.mailkube.com/mta/v1/scheduled-emails?page=2"}},
		"data":[{"id":"se_1"},{"id":"se_2"}]}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: first}}}
	client := stub.client(t)

	count := 0
	for range client.ScheduledEmails.All(context.Background(), mailkube.ScheduledEmailListParams{}) {
		count++
		break
	}

	if count != 1 {
		t.Errorf("yielded %d items after a break, want 1", count)
	}
	if len(stub.calls) != 1 {
		t.Errorf("made %d calls after a break, want 1", len(stub.calls))
	}
}

func TestAllYieldsTheErrorOnceAndStops(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 404, payload: `{"name":"not_found"}`}}}
	client := stub.client(t)

	yields := 0
	for item, err := range client.ScheduledEmails.All(context.Background(), mailkube.ScheduledEmailListParams{}) {
		yields++
		if item != nil {
			t.Error("an error yield must carry a nil item")
		}
		if !errors.Is(err, mailkube.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	}

	if yields != 1 {
		t.Errorf("yielded %d times, want 1", yields)
	}
}

func TestAllRefusesAPageLinkOffTheConfiguredOrigin(t *testing.T) {
	first := `{"pagination":{"steps":{"next":"https://evil.example.com/scheduled-emails?page=2"}},
		"data":[{"id":"se_1"}]}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: first}}}
	client := stub.client(t)

	var lastErr error
	for _, err := range client.ScheduledEmails.All(context.Background(), mailkube.ScheduledEmailListParams{}) {
		lastErr = err
	}

	if lastErr == nil || !strings.Contains(lastErr.Error(), "not on the configured API origin") {
		t.Errorf("err = %v, want the origin guard to refuse the link", lastErr)
	}
	if len(stub.calls) != 1 {
		t.Errorf("made %d calls, want 1: the foreign link must never be requested", len(stub.calls))
	}
}

func TestGetRetrievesOneScheduledEmail(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: scheduledEmailPayload}}}

	item, err := stub.client(t).ScheduledEmails.Get(context.Background(), "se_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodGet || call.path != "/mta/v1/scheduled-emails/se_1" {
		t.Errorf("call = %s %s", call.method, call.path)
	}
	if item.Status != "scheduled" || item.BatchID != "launch" {
		t.Errorf("item = %+v", item)
	}
}

func TestAnIdentifierStaysInsideItsRoute(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: scheduledEmailPayload}}}

	if _, err := stub.client(t).ScheduledEmails.Get(context.Background(), "a/b?c"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	call := stub.only(t)
	if call.path != "/mta/v1/scheduled-emails/a%2Fb%3Fc" {
		t.Errorf("path = %q, want the identifier escaped into one segment", call.path)
	}
	if call.query != "" {
		t.Errorf("query = %q, want empty: the identifier must not leak into the query", call.query)
	}
}

func TestUpdateReschedulesWithPatch(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: scheduledEmailPayload}}}

	_, err := stub.client(t).ScheduledEmails.Update(context.Background(), "se_1", mailkube.ScheduledEmailUpdateParams{
		ScheduledAt: time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC),
		BatchID:     "launch",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodPatch || call.path != "/mta/v1/scheduled-emails/se_1" {
		t.Errorf("call = %s %s", call.method, call.path)
	}
	if call.body["scheduled_at"] != "2026-08-21T07:00:00Z" || call.body["batch_id"] != "launch" {
		t.Errorf("body = %v", call.body)
	}
}

func TestUpdateOmitsAnUnsetBatch(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: scheduledEmailPayload}}}

	_, err := stub.client(t).ScheduledEmails.Update(context.Background(), "se_1", mailkube.ScheduledEmailUpdateParams{
		ScheduledAt: time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, present := stub.only(t).body["batch_id"]; present {
		t.Error("body unexpectedly contains batch_id")
	}
}

func TestCancelSendsADeleteWithNoBody(t *testing.T) {
	payload := `{"id":"se_1","object":"scheduled_email","status":"canceled"}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: payload}}}

	canceled, err := stub.client(t).ScheduledEmails.Cancel(context.Background(), "se_1")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodDelete || call.path != "/mta/v1/scheduled-emails/se_1" {
		t.Errorf("call = %s %s", call.method, call.path)
	}
	if call.body != nil {
		t.Errorf("body = %v, want none", call.body)
	}
	if canceled.Status != "canceled" {
		t.Errorf("status = %q", canceled.Status)
	}
}

func TestBatchesUpdateTargetsTheBatchPath(t *testing.T) {
	payload := `{"object":"scheduled_email.batch","batch_id":"launch","rescheduled_count":4,
		"scheduled_at":"2026-08-21T07:00:00Z"}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: payload}}}

	result, err := stub.client(t).ScheduledEmails.Batches.Update(
		context.Background(), "launch", mailkube.ScheduledEmailBatchUpdateParams{
			ScheduledAt: time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC),
		})
	if err != nil {
		t.Fatalf("Batches.Update: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodPatch || call.path != "/mta/v1/scheduled-emails/batches/launch" {
		t.Errorf("call = %s %s", call.method, call.path)
	}
	// The batch is identified by the path; a body batch_id is silently ignored by the server,
	// so the SDK must never send one.
	if _, present := call.body["batch_id"]; present {
		t.Error("body unexpectedly contains batch_id")
	}
	if result.RescheduledCount != 4 {
		t.Errorf("rescheduled_count = %d", result.RescheduledCount)
	}
}

func TestBatchesCancelReportsAnUnknownBatchAsZeroRatherThanAnError(t *testing.T) {
	payload := `{"object":"scheduled_email.batch","batch_id":"nope","canceled_count":0}`
	stub := &stubSequence{replies: []stubReply{{status: 200, payload: payload}}}

	result, err := stub.client(t).ScheduledEmails.Batches.Cancel(context.Background(), "nope")
	if err != nil {
		t.Fatalf("Batches.Cancel: %v", err)
	}

	call := stub.only(t)
	if call.method != http.MethodDelete || call.path != "/mta/v1/scheduled-emails/batches/nope" {
		t.Errorf("call = %s %s", call.method, call.path)
	}
	if result.CanceledCount != 0 {
		t.Errorf("canceled_count = %d", result.CanceledCount)
	}
}

func TestAScheduledVerbMapsItsStatusToACategory(t *testing.T) {
	stub := &stubSequence{replies: []stubReply{
		{status: 404, payload: `{"name":"scheduled_email_not_found","message":"gone"}`},
	}}

	_, err := stub.client(t).ScheduledEmails.Get(context.Background(), "se_1")
	if !errors.Is(err, mailkube.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	var apiErr *mailkube.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorName != mailkube.ErrorNameScheduledEmailNotFound {
		t.Errorf("error name = %q", apiErr.ErrorName)
	}
}

func TestAMalformedSuccessBodyIsNotReportedAsSuccess(t *testing.T) {
	for name, payload := range map[string]string{
		"empty":      "",
		"non object": `["nope"]`,
		"truncated":  `{"id":`,
		// `null` is the one that decodes *successfully* into a zero-valued struct, so without an
		// explicit object check the caller is handed a CanceledScheduledEmail with no id and told
		// the cancel worked.
		"null":   "null",
		"scalar": "42",
	} {
		t.Run(name, func(t *testing.T) {
			stub := &stubSequence{replies: []stubReply{{status: 200, payload: payload}}}

			_, err := stub.client(t).ScheduledEmails.Cancel(context.Background(), "se_1")
			if !errors.Is(err, mailkube.ErrUnexpectedResponse) {
				t.Errorf("err = %v, want ErrUnexpectedResponse", err)
			}
			if errors.Is(err, mailkube.ErrAPI) {
				t.Error("a malformed 2xx body must not be reported as an API error")
			}
		})
	}
}
