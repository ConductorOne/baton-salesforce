package client

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"time"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserCreateRequest struct {
	Email       string
	Alias       string
	LastName    string
	FirstName   string
	ProfileId   string
	TimeZoneSid string
}

func (c *SalesforceClient) CreateUser(ctx context.Context, request UserCreateRequest) error {
	_, err := mail.ParseAddress(request.Email)
	if err != nil {
		return fmt.Errorf("baton-salesforce: invalid user email: %w", err)
	}

	_, err = time.LoadLocation(request.TimeZoneSid)
	if err != nil {
		return fmt.Errorf("baton-salesforce: invalid timezone: %w", err)
	}

	userData := map[string]interface{}{
		"Username":          request.Email,
		"Alias":             request.Alias,
		"Email":             request.Email,
		"LastName":          request.LastName,
		"FirstName":         request.FirstName,
		"TimeZoneSidKey":    request.TimeZoneSid,
		"ProfileId":         request.ProfileId,
		"EmailEncodingKey":  "UTF-8",
		"LocaleSidKey":      "en_US",
		"LanguageLocaleKey": "en_US",
		"ContactId":         nil,
	}

	// We dont need rate limit data since err returns the rate limit data by uhttp
	_, err = c.CreateObject(
		ctx,
		TableNameUsers,
		userData,
	)

	return err
}

func (c *SalesforceClient) UserExist(
	ctx context.Context,
	email string,
) (
	bool,
	error,
) {
	user, err := c.GetUserByEmail(ctx, email)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, err
	}

	return user != nil, nil
}

func (c *SalesforceClient) GetUserByEmailWithRetry(
	ctx context.Context,
	email string,
) (
	*SalesforceUser,
	error,
) {
	maxRetries := 3
	baseDelay := time.Second
	var err error

	for attempt := range maxRetries {
		// Clear cache or we won't get new responses
		err = uhttp.ClearCaches(ctx)
		if err != nil {
			return nil, err
		}
		user, err := c.GetUserByEmail(ctx, email)
		if err == nil {
			return user, nil
		}

		if status.Code(err) != codes.NotFound {
			return nil, err
		}
		if attempt < maxRetries-1 {
			delay := time.Duration(attempt+1) * baseDelay
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// continue to next attempt
			}
		}
	}

	return nil, fmt.Errorf("failed to get user by email after %d retries", maxRetries)
}

func (c *SalesforceClient) GetUserByEmail(
	ctx context.Context,
	email string,
) (
	*SalesforceUser,
	error,
) {
	// Filter for Standard users only - these are full Salesforce users with standard licenses.
	// Other types like Partner, Portal, or Chatter users have limited access and are excluded.
	// See https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_user.htm
	// While we're not syncing Chatter users, we need to support creating them so we can possibly then upgrade them.
	// this is slightly Weird behavior but there's a future where we do a more nuanced thing here.
	query := NewQuery(TableNameUsers).
		WhereEq("Email", email)
	records, _, _, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, err
	}
	users := make([]*SalesforceUser, 0, 1)
	for _, record := range records {
		isActive, err := getIsActive(record)
		if err != nil {
			return nil, err
		}
		if shouldSkipSyncingUserType(ctx, record) {
			continue
		}

		lastLogin, err := parseSalesforceDatetime(record.StringField("LastLoginDate"))
		if err != nil {
			return nil, err
		}

		users = append(users, &SalesforceUser{
			ID:            record.ID(),
			Username:      record.StringField("Username"),
			Email:         record.StringField("Email"),
			FirstName:     record.StringField("FirstName"),
			LastName:      record.StringField("LastName"),
			UserType:      record.StringField("UserType"),
			IsActive:      isActive,
			LastLoginDate: lastLogin,
		})
	}

	if len(users) == 0 {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("user with email %s not found", email))
	}

	return users[0], nil
}

// SendResetPasswordEmail sends a reset password email to the user with the given ID.
// https://developer.salesforce.com/docs/atlas.en-us.api_rest.meta/api_rest/resources_sobject_user_password_delete.htm
func (c *SalesforceClient) SendResetPasswordEmail(ctx context.Context, userId string) error {
	resetPath := fmt.Sprintf(ResetPasswordPath, userId)

	_, err := c.client.ApexREST(ctx, http.MethodDelete, resetPath, nil)
	if err != nil {
		return err
	}

	return nil
}
