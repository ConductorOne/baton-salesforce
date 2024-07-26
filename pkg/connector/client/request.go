package client

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/simpleforce/simpleforce"
	"go.uber.org/zap"
)

func getQueryString(
	q *SalesforceQuery,
	paginationPath string,
	pageSize int,
) string {
	if paginationPath != "" {
		return paginationPath
	}

	if pageSize <= 0 {
		pageSize = PageSizeDefault
	}

	return q.
		OrderBy(SalesforcePK).
		Limit(pageSize).
		String()
}

// query performs a query to the Salesforce API via the simpleforce client. When
// passed a pagination URL, it just hits that endpoint. Otherwise, it builds the
// SOQL query string and hits the query endpoint with it. Either way, the method
// returns a list of `SObject` records, ratelimit data gleaned from headers, and
// the next pagination URL.
func (c *SalesforceClient) query(
	ctx context.Context,
	query *SalesforceQuery,
	paginationPath string,
	pageSize int,
) (
	[]simpleforce.SObject,
	string,
	*v2.RateLimitDescription,
	error,
) {
	logger := ctxzap.Extract(ctx)
	queryString := getQueryString(query, paginationPath, pageSize)
	records, err := c.client.Query(queryString)
	ratelimitData := c.salesforceTransport.rateLimit
	if err != nil {
		logger.Error(
			"salesforce-connector: error querying salesforce",
			zap.String("query", queryString),
			zap.Error(err),
		)
		return nil, "", ratelimitData, err
	}

	nextToken := ""
	if !records.Done {
		nextToken = records.NextRecordsURL
	}
	return records.Records, nextToken, ratelimitData, nil
}
