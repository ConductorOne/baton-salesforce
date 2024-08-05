package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/resource"
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
	shouldUseUsernameForEmail bool,
) (*v2.Resource, error) {
	displayName := fmt.Sprintf(
		"%s %s",
		user.FirstName,
		user.LastName,
	)
	status := v2.UserTrait_Status_STATUS_DISABLED
	if user.IsActive {
		status = v2.UserTrait_Status_STATUS_ENABLED
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
		newResource, err := userResource(
			ctx,
			user,
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
