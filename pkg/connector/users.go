package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type userBuilder struct {
	resourceType              *v2.ResourceType
	client                    *client.SalesforceClient
	shouldUseUsernameForEmail bool
}

// userResource convert a SalesforceUser into a Resource.
func userResource(
	ctx context.Context,
	user *client.SalesforceUser,
	userLogin *client.UserLogin,
	shouldUseUsernameForEmail bool,
) (*v2.Resource, error) {
	displayName := fmt.Sprintf(
		"%s %s",
		user.FirstName,
		user.LastName,
	)
	status := v2.UserTrait_Status_STATUS_DISABLED

	if user.IsActive {
		if userLogin != nil && userLogin.IsFrozen {
			status = v2.UserTrait_Status_STATUS_DISABLED
		} else {
			status = v2.UserTrait_Status_STATUS_ENABLED
		}
	}

	email := user.Email
	if shouldUseUsernameForEmail {
		email = user.Username
	}

	profile := map[string]interface{}{
		"full_name":    displayName,
		"username":     user.Username,
		"account_type": user.UserType,
		"email":        email,
		"id":           user.ID,
	}

	userTraitOptions := []resource.UserTraitOption{
		resource.WithUserProfile(profile),
		resource.WithEmail(email, true),
		resource.WithStatus(status),
		resource.WithUserLogin(user.Username),
	}

	if user.LastLoginDate != nil {
		userTraitOptions = append(userTraitOptions, resource.WithLastLogin(*user.LastLoginDate))
	}

	newUserResource, err := resource.NewUserResource(
		displayName,
		resourceTypeUser,
		user.ID,
		userTraitOptions,
	)
	if err != nil {
		return nil, err
	}

	return newUserResource, nil
}

func (o *userBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeUser
}

// List returns all the users from the database as resource objects.
// Users include a UserTrait because they are the 'shape' of a standard user.
func (o *userBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	pToken *pagination.Token,
) (
	[]*v2.Resource,
	string,
	annotations.Annotations,
	error,
) {
	users, nextToken, ratelimitData, err := o.client.GetUsers(
		ctx,
		pToken.Token,
		pToken.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, "", outputAnnotations, err
	}

	rv := make([]*v2.Resource, 0)
	for _, user := range users {
		userLogin, _, err := o.client.GetUserLogin(ctx, user.ID)
		if err != nil {
			return nil, "", nil, err
		}

		newResource, err := userResource(
			ctx,
			user,
			userLogin,
			o.shouldUseUsernameForEmail,
		)
		if err != nil {
			return nil, "", outputAnnotations, err
		}

		rv = append(rv, newResource)
	}
	return rv, nextToken, outputAnnotations, nil
}

// Entitlements always returns an empty slice for users.
func (o *userBuilder) Entitlements(
	_ context.Context,
	resource *v2.Resource,
	_ *pagination.Token,
) (
	[]*v2.Entitlement,
	string,
	annotations.Annotations,
	error,
) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (o *userBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	pToken *pagination.Token,
) (
	[]*v2.Grant,
	string,
	annotations.Annotations,
	error,
) {
	return nil, "", nil, nil
}

func getUserCreateRequestParams(accountInfo *v2.AccountInfo) (*client.UserCreateRequest, error) {
	email, ok := resource.GetProfileStringValue(accountInfo.Profile, "email")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing email in account info")
	}

	alias, ok := resource.GetProfileStringValue(accountInfo.Profile, "alias")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing alias in account info")
	}

	firstName, ok := resource.GetProfileStringValue(accountInfo.Profile, "first_name")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing first_name in account info")
	}

	lastName, ok := resource.GetProfileStringValue(accountInfo.Profile, "last_name")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing last_name in account info")
	}

	profileId, ok := resource.GetProfileStringValue(accountInfo.Profile, "profileId")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing profileId in account info")
	}

	timezone, ok := resource.GetProfileStringValue(accountInfo.Profile, "timezone")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing timezone in account info")
	}

	return &client.UserCreateRequest{
		Email:       email,
		Alias:       alias,
		TimeZoneSid: timezone,
		ProfileId:   profileId,
		FirstName:   firstName,
		LastName:    lastName,
	}, nil
}

func (o *userBuilder) CreateAccount(
	ctx context.Context,
	accountInfo *v2.AccountInfo,
	credentialOptions *v2.CredentialOptions,
) (
	connectorbuilder.CreateAccountResponse,
	[]*v2.PlaintextData,
	annotations.Annotations,
	error,
) {
	l := ctxzap.Extract(ctx)

	userRequest, err := getUserCreateRequestParams(accountInfo)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("baton-salesforce: create account get InviteUserParams failed %w", err)
	}

	userExist, err := o.client.UserExist(ctx, userRequest.Email)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("baton-salesforce: check if user exists failed %w", err)
	}

	if userExist {
		// l.Info("User already exists, skipping user creation")
		user, err := o.client.GetUserByEmail(ctx, userRequest.Email)
		if err != nil {
			return nil, nil, nil, err
		}

		if !user.IsActive {
			rl, err := o.client.SetUserActiveState(ctx, user.ID, true)
			outputAnnotations := client.WithRateLimitAnnotations(rl)
			if err != nil {
				return nil, nil, outputAnnotations, err
			}
			l.Info("User is inactive; activating user", zap.String("email", userRequest.Email), zap.String("user_id", user.ID))
		} else {
			l.Info("User already exists, skipping user creation")
		}
	} else {
		err = o.client.CreateUser(ctx, *userRequest)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Clear cache to find the new user by email, otherwise it will not be found
	err = uhttp.ClearCaches(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	user, err := o.client.GetUserByEmail(ctx, userRequest.Email)
	if err != nil {
		return nil, nil, nil, err
	}

	l.Info("Sending reset password email", zap.String("email", user.Email))
	err = o.client.SendResetPasswordEmail(ctx, user.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	l.Debug("Reset password email sent", zap.String("email", user.Email), zap.String("user_id", user.ID))

	userLogin, _, err := o.client.GetUserLogin(ctx, user.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	r, err := userResource(ctx, user, userLogin, o.shouldUseUsernameForEmail)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("baton-salesforce: cannot create user resource: %w", err)
	}

	return &v2.CreateAccountResponse_SuccessResult{
		Resource:              r,
		IsCreateAccountResult: true,
	}, nil, nil, nil
}

func (o *userBuilder) CreateAccountCapabilityDetails(ctx context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

func newUserBuilder(
	client *client.SalesforceClient,
	shouldUseUsernameForEmail bool,
) *userBuilder {
	return &userBuilder{
		resourceType:              resourceTypeUser,
		client:                    client,
		shouldUseUsernameForEmail: shouldUseUsernameForEmail,
	}
}
