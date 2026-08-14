package mailkube

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	scheduledPath = "scheduled-emails"
	batchPath     = "scheduled-emails/batches"
)

// rescheduleBody is the wire body shared by the single-email and batch reschedule verbs.
type rescheduleBody struct {
	ScheduledAt string `json:"scheduled_at"`
	BatchID     string `json:"batch_id,omitempty"`
}

// listSpec builds the request listing scheduled emails.
//
// Every zero-valued filter is omitted rather than sent. That is not cosmetic for Page: the
// server requires page >= 1, so emitting the struct's zero value would turn the most obvious
// first call, List(ctx, ScheduledEmailListParams{}), into a validation error.
func listSpec(params ScheduledEmailListParams) requestSpec {
	query := url.Values{}
	if len(params.Status) > 0 {
		// Comma-joined rather than repeated: the server accepts both and normalizes, and one
		// value per key keeps the query flat.
		query.Set("status", strings.Join(params.Status, ","))
	}
	if params.BatchID != "" {
		query.Set("batch_id", params.BatchID)
	}
	if rendered := renderTime(params.ScheduledAtGTE); rendered != "" {
		query.Set("scheduled_at_gte", rendered)
	}
	if rendered := renderTime(params.ScheduledAtLTE); rendered != "" {
		query.Set("scheduled_at_lte", rendered)
	}
	if params.Page > 0 {
		query.Set("page", strconv.Itoa(params.Page))
	}
	return requestSpec{path: scheduledPath, method: http.MethodGet, query: query}
}

// nextPageSpec builds the request for the page after the one in hand, or reports that there is
// none.
//
// The API issues absolute page links, which the client only follows when they are on its own
// origin, so a link cannot redirect a credentialed request elsewhere.
func nextPageSpec(page *ScheduledEmailPage) (requestSpec, bool) {
	if page.Pagination.Steps.Next == "" {
		return requestSpec{}, false
	}
	return requestSpec{path: page.Pagination.Steps.Next, method: http.MethodGet}, true
}

// getSpec builds the request retrieving one scheduled email.
func getSpec(emailID string) requestSpec {
	return requestSpec{path: itemPath(scheduledPath, emailID), method: http.MethodGet}
}

// updateSpec builds the request rescheduling one scheduled email.
func updateSpec(emailID string, params ScheduledEmailUpdateParams) requestSpec {
	return requestSpec{
		path:   itemPath(scheduledPath, emailID),
		method: http.MethodPatch,
		body:   rescheduleBody{ScheduledAt: renderTime(params.ScheduledAt), BatchID: params.BatchID},
	}
}

// cancelSpec builds the request canceling one scheduled email.
func cancelSpec(emailID string) requestSpec {
	return requestSpec{path: itemPath(scheduledPath, emailID), method: http.MethodDelete}
}

// batchUpdateSpec builds the request rescheduling a whole batch.
func batchUpdateSpec(batchID string, params ScheduledEmailBatchUpdateParams) requestSpec {
	return requestSpec{
		path:   itemPath(batchPath, batchID),
		method: http.MethodPatch,
		body:   rescheduleBody{ScheduledAt: renderTime(params.ScheduledAt)},
	}
}

// batchCancelSpec builds the request canceling a whole batch.
func batchCancelSpec(batchID string) requestSpec {
	return requestSpec{path: itemPath(batchPath, batchID), method: http.MethodDelete}
}

// newScheduledEmailsService binds the namespace and its batch sub-namespace to a transport.
func newScheduledEmailsService(transport jsonTransport) *ScheduledEmailsService {
	return &ScheduledEmailsService{
		Batches:   &ScheduledEmailBatchesService{transport: transport},
		transport: transport,
	}
}

// ScheduledEmailsService is the scheduled-emails namespace, reached as client.ScheduledEmails.
//
// A send carrying SendEmailParams.ScheduledAt is accepted but not delivered yet; until it is
// due it lives in this collection, where it can be listed, inspected, rescheduled or canceled,
// one at a time or a whole batch at once through Batches.
type ScheduledEmailsService struct {
	// Batches is the batch namespace, reached as client.ScheduledEmails.Batches.
	Batches *ScheduledEmailBatchesService

	transport jsonTransport
}

// List returns one page of scheduled emails.
//
// Use All to walk every page.
func (s *ScheduledEmailsService) List(
	ctx context.Context, params ScheduledEmailListParams,
) (*ScheduledEmailPage, error) {
	return fetch[ScheduledEmailPage](ctx, s.transport, listSpec(params))
}

// All iterates every scheduled email matching the filters, across all pages.
//
// Pages are fetched lazily by following the links the API returns, so abandoning the loop early
// costs nothing and leaks nothing: a range-over-func iterator runs on the calling goroutine.
// A failure is yielded once, as the second value, and ends the iteration:
//
//	for item, err := range client.ScheduledEmails.All(ctx, params) {
//		if err != nil {
//			return err
//		}
//		fmt.Println(item.ID)
//	}
//
// Ranging over the result needs go 1.23 or later in your own go.mod, not just in your
// toolchain. On an older module, page by hand with List and Pagination.Steps.Next.
func (s *ScheduledEmailsService) All(
	ctx context.Context, params ScheduledEmailListParams,
) iter.Seq2[*ScheduledEmail, error] {
	return func(yield func(*ScheduledEmail, error) bool) {
		spec := listSpec(params)
		for {
			page, err := fetch[ScheduledEmailPage](ctx, s.transport, spec)
			if err != nil {
				yield(nil, err)
				return
			}
			for i := range page.Data {
				if !yield(&page.Data[i], nil) {
					return
				}
			}
			next, ok := nextPageSpec(page)
			if !ok {
				return
			}
			spec = next
		}
	}
}

// Get retrieves one scheduled email by the id the send acknowledgement returned.
func (s *ScheduledEmailsService) Get(ctx context.Context, emailID string) (*ScheduledEmail, error) {
	return fetch[ScheduledEmail](ctx, s.transport, getSpec(emailID))
}

// Update reschedules one scheduled email, optionally moving it into a batch.
func (s *ScheduledEmailsService) Update(
	ctx context.Context, emailID string, params ScheduledEmailUpdateParams,
) (*ScheduledEmail, error) {
	return fetch[ScheduledEmail](ctx, s.transport, updateSpec(emailID, params))
}

// Cancel cancels one scheduled email.
func (s *ScheduledEmailsService) Cancel(
	ctx context.Context, emailID string,
) (*CanceledScheduledEmail, error) {
	return fetch[CanceledScheduledEmail](ctx, s.transport, cancelSpec(emailID))
}

// ScheduledEmailBatchesService is the batch namespace, reached as
// client.ScheduledEmails.Batches.
type ScheduledEmailBatchesService struct {
	transport jsonTransport
}

// Update reschedules every pending email in a batch.
//
// An unknown batch is a no-op reporting RescheduledCount 0, not an error.
func (s *ScheduledEmailBatchesService) Update(
	ctx context.Context, batchID string, params ScheduledEmailBatchUpdateParams,
) (*ScheduledEmailBatchUpdate, error) {
	return fetch[ScheduledEmailBatchUpdate](ctx, s.transport, batchUpdateSpec(batchID, params))
}

// Cancel cancels every pending email in a batch.
//
// An unknown batch is a no-op reporting CanceledCount 0, not an error.
func (s *ScheduledEmailBatchesService) Cancel(
	ctx context.Context, batchID string,
) (*ScheduledEmailBatchCancel, error) {
	return fetch[ScheduledEmailBatchCancel](ctx, s.transport, batchCancelSpec(batchID))
}
