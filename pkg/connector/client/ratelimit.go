package client

import (
	"fmt"
	"net/http"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
)

const (
	rateLimitHeaderKey = "Sforce-Limit-Info"
	rateLimitFmt       = "api-usage=%d/%d"
)

// RoundTrip TODO MARCOS DESCRIBE
func (t *salesforceHttpTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.rateLimit = nil // clear previous
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if rateLimitInfo, ok := response.Header[rateLimitHeaderKey]; ok && len(rateLimitInfo) == 1 {
		var remaining int64
		var limit int64
		if found, err := fmt.Sscanf(rateLimitInfo[0], rateLimitFmt, &remaining, &limit); err == nil && found == 2 {
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