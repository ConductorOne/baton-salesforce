package client

import (
	"fmt"
	"net/http"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
)

const (
	RateLimitHeaderKey = "Sforce-Limit-Info"
	RateLimitFmt       = "api-usage=%d/%d"

	// QueryBatchSize caps the number of records Salesforce returns per /query
	// batch so the caller's follow-up per-record work (e.g. GetUserLogin)
	// completes inside a single connector invocation's time budget.
	// Valid range per Salesforce docs is 200–2000; the header is honored on
	// the initial /query call and carried through subsequent nextRecordsUrl fetches.
	QueryBatchSize         = 200
	QueryOptionsHeaderKey  = "Sforce-Query-Options"
	queryOptionsBatchSize  = "batchSize=%d"
	salesforceQueryURLPart = "/services/data/"
)

// RoundTrip - the simpleforce interface doesn't expose HTTP headers to us, but
// we can still reach them if we create a wrapper to the `.RoundTrip()` method.
// This just calls the original `httpClient.RoundTrip()` and caches the header
// value that Salesforce uses to communicate remaining API call counts.
func (t *salesforceHttpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.rateLimit = nil // clear previous

	// Bound server-side query batch size so per-record follow-up work fits
	// in one invocation. Applied to the initial /query request; subsequent
	// nextRecordsUrl fetches inherit the batch size from the initial call.
	if strings.Contains(request.URL.Path, salesforceQueryURLPart) &&
		strings.Contains(request.URL.Path, "/query") &&
		request.Header.Get(QueryOptionsHeaderKey) == "" {
		request.Header.Set(QueryOptionsHeaderKey, fmt.Sprintf(queryOptionsBatchSize, QueryBatchSize))
	}

	response, err := t.base.RoundTrip(request)
	if err != nil {
		return response, err
	}

	if rateLimitInfo, ok := response.Header[RateLimitHeaderKey]; ok && len(rateLimitInfo) == 1 {
		var remaining int64
		var limit int64
		if found, err := fmt.Sscanf(rateLimitInfo[0], RateLimitFmt, &remaining, &limit); err == nil && found == 2 {
			t.rateLimit = &v2.RateLimitDescription{
				Status:    v2.RateLimitDescription_STATUS_OK,
				Limit:     limit,
				Remaining: remaining,
				// The limit is moving a 24-hour window for total requests. It
				// doesn't reset to 0. Whenever under limit, it can make more
				// requests. When more requests are available is not tracked.
				// https://developer.salesforce.com/docs/atlas.en-us.salesforce_app_limits_cheatsheet.meta/salesforce_app_limits_cheatsheet/salesforce_app_limits_platform_api.htm
				ResetAt: nil,
			}
			if remaining > limit {
				t.rateLimit.Status = v2.RateLimitDescription_STATUS_OVERLIMIT
			}
		}
	}
	return response, nil
}

func WithRateLimitAnnotations(
	ratelimitDescriptionAnnotations ...*v2.RateLimitDescription,
) annotations.Annotations {
	outputAnnotations := annotations.Annotations{}
	for _, annotation := range ratelimitDescriptionAnnotations {
		outputAnnotations.Append(annotation)
	}

	return outputAnnotations
}
