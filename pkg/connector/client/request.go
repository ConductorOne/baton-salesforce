package client

import (
	"context"
	"errors"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/simpleforce"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var ObjectNotFound = errors.New("Salesforce object does not exists")

func getQueryString(
	q *SalesforceQuery,
	paginationPath string,
	pageSize int,
) string {
	if paginationPath != "" {
		return paginationPath
	}
	return q.
		OrderBy(SalesforcePK).
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
	err := c.Initialize(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	logger := ctxzap.Extract(ctx)
	queryString := getQueryString(query, paginationPath, pageSize)
	records, err := c.client.Query(ctx, queryString)
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

func (c *SalesforceClient) getSObject(
	ctx context.Context,
	query *SalesforceQuery,
) (
	*simpleforce.SObject,
	*v2.RateLimitDescription,
	error,
) {
	logger := ctxzap.Extract(ctx)
	records, _, ratelimitData, err := c.query(
		ctx,
		query,
		"",
		-1,
	)
	if err != nil {
		return nil, ratelimitData, err
	}

	if len(records) == 0 {
		return nil, ratelimitData, ObjectNotFound
	}

	if len(records) > 1 {
		logger.Error(
			"found too many Salesforce objects",
			zap.String("query", query.String()),
			zap.Int("count", len(records)),
		)
	}

	return &records[0], ratelimitData, nil
}

// CreateObject this call to simpleforce is broken out into a helper function so
// that we can always ensure that the client is initialized.
func (c *SalesforceClient) CreateObject(
	ctx context.Context,
	tableName string,
	values map[string]interface{},
) (*v2.RateLimitDescription, error) {
	logger := ctxzap.Extract(ctx)
	logger.Debug(
		"Starting CreateObject",
		zap.String("tableName", tableName),
	)

	err := c.Initialize(ctx)
	if err != nil {
		return nil, err
	}

	created := c.client.SObject(tableName)
	for key, value := range values {
		created = created.Set(key, value)
	}
	created, err = created.Create(ctx)
	if err != nil {
		return nil, err
	}

	debugFields := []zap.Field{}
	if created != nil {
		debugFields = append(debugFields, zap.String("created.ID()", created.ID()))
	}
	logger.Debug(
		"Called Create()",
		debugFields...,
	)

	ratelimitData := c.salesforceTransport.rateLimit
	if created == nil {
		return ratelimitData, fmt.Errorf("failed to create object")
	}
	return ratelimitData, nil
}

func (c *SalesforceClient) DeleteObject(
	ctx context.Context,
	tableName string,
	id string,
) (*v2.RateLimitDescription, error) {
	logger := ctxzap.Extract(ctx)
	logger.Debug(
		"Starting DeleteObject",
		zap.String("tableName", tableName),
	)

	err := c.Initialize(ctx)
	if err != nil {
		return nil, err
	}

	// TODO(marcos): There is a bug in simpleforce that prevents us from doing
	// `found.Delete()`. See https://github.com/simpleforce/simpleforce/pull/44.
	err = c.client.
		SObject(tableName).
		Set("Id", id).
		Delete(ctx)

	ratelimitData := c.salesforceTransport.rateLimit
	return ratelimitData, err
}

func (c *SalesforceClient) getOneUser(ctx context.Context, userId string) (
	*simpleforce.SObject,
	*v2.RateLimitDescription,
	error,
) {
	err := c.Initialize(ctx)
	if err != nil {
		return nil, nil, err
	}
	user, err := c.client.
		SObject(TableNameUsers).
		Get(ctx, userId)
	if err != nil {
		return nil, nil, err
	}

	ratelimitData := c.salesforceTransport.rateLimit
	if user == nil {
		return nil, ratelimitData, fmt.Errorf("missing user %s", userId)
	}
	return user, ratelimitData, nil
}

func (c *SalesforceClient) updateUser(
	ctx context.Context,
	user *simpleforce.SObject,
	fieldName string,
	value string,
) (
	*v2.RateLimitDescription,
	error,
) {
	user, err := user.Set(fieldName, value).Update(ctx)
	ratelimitData := c.salesforceTransport.rateLimit
	if err != nil {
		return ratelimitData, err
	}

	if user == nil {
		return ratelimitData, fmt.Errorf("failed to update user")
	}

	return ratelimitData, nil
}

func (c *SalesforceClient) setValue(
	ctx context.Context,
	userId string,
	fieldName string,
	fieldValue string,
) (*v2.RateLimitDescription, error) {
	user, ratelimitData, err := c.getOneUser(ctx, userId)
	if err != nil {
		return ratelimitData, err
	}

	return c.updateUser(ctx, user, fieldName, fieldValue)
}

func (c *SalesforceClient) setOneValue(
	ctx context.Context,
	userId string,
	fieldName string,
	fieldValue string,
) (*v2.RateLimitDescription, error) {
	user, ratelimitData, err := c.getOneUser(ctx, userId)
	if err != nil {
		return ratelimitData, err
	}

	copySObject, err := c.copySObject(user, fieldName)
	if err != nil {
		return nil, err
	}

	return c.updateUser(ctx, copySObject, fieldName, fieldValue)
}

func (c *SalesforceClient) clearValue(
	ctx context.Context,
	userId string,
	fieldName string,
	fieldValue string,
) (*v2.RateLimitDescription, error) {
	user, ratelimitData, err := c.getOneUser(ctx, userId)
	if err != nil {
		return ratelimitData, err
	}

	if user.StringField(fieldName) != fieldValue {
		return nil, fmt.Errorf("missing %s: %s", fieldName, fieldValue)
	}

	return c.updateUser(ctx, user, fieldName, "")
}

// clearOneValue clears a single value from a user record.
func (c *SalesforceClient) clearOneValue(
	ctx context.Context,
	userId string,
	fieldName string,
	fieldValue string,
) (*v2.RateLimitDescription, error) {
	user, ratelimitData, err := c.getOneUser(ctx, userId)
	if err != nil {
		return ratelimitData, err
	}

	if user.StringField(fieldName) != fieldValue {
		return nil, fmt.Errorf("missing %s: %s", fieldName, fieldValue)
	}

	copySObject, err := c.copySObject(user, fieldName)
	if err != nil {
		return nil, err
	}

	return c.updateUser(ctx, copySObject, fieldName, "")
}

func (c *SalesforceClient) copySObject(obj *simpleforce.SObject, allowedFields ...string) (*simpleforce.SObject, error) {
	response := c.client.SObject(obj.Type())

	response.Set("Id", obj.ID())
	if obj.ExternalIDFieldName() != "" {
		response.Set(obj.ExternalIDFieldName(), obj.ExternalID())
	}

	for _, field := range allowedFields {
		if obj.StringField(field) == "" {
			continue
		}

		response.Set(field, obj.StringField(field))
	}

	return response, nil
}
