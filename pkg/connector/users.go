package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
)

type userBuilder struct {
	resourceType              *v2.ResourceType
	client                    *client.SalesforceClient
	shouldUseUsernameForEmail bool
	syncDeactivatedUsers      bool
	syncNonStandardUsers      bool
}

var _ connectorbuilder.AccountManagerV2 = &userBuilder{}

// accountTypeForUser maps a Salesforce user to the NHI account-type spine.
//
// Two non-human signals are recognized: the user is the runtime identity of an
// Einstein Bot / Agentforce Agent (referenced by BotDefinition.BotUserId — a
// stable foreign key, the reliable discriminator for an "Einstein Agent User"),
// or it is the Automated Process system user (alias "autoproc", UserType
// "AutomatedProcess"). Either is SERVICE; every other synced user is HUMAN. The
// SDK coerces an unset account type to HUMAN on the wire, so this positively
// emits the value rather than relying on that default.
func accountTypeForUser(userType string, isAgentUser bool) v2.UserTrait_AccountType {
	if isAgentUser || userType == "AutomatedProcess" {
		return v2.UserTrait_ACCOUNT_TYPE_SERVICE
	}
	return v2.UserTrait_ACCOUNT_TYPE_HUMAN
}

// userResource convert a SalesforceUser into a Resource. isAgentUser is true
// when the user backs an Einstein Bot / Agentforce Agent (BotDefinition.BotUserId).
func userResource(
	ctx context.Context,
	user *client.SalesforceUser,
	userLogin *client.UserLogin,
	shouldUseUsernameForEmail bool,
	isAgentUser bool,
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
			ctxzap.Extract(ctx).Debug(
				"salesforce-connector: marking active user disabled because UserLogin.IsFrozen is true",
				zap.String("user_id", user.ID),
			)
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

	userTraitOptions := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithEmail(email, true),
		rs.WithStatus(status),
		rs.WithUserLogin(user.Username),
		rs.WithAccountType(accountTypeForUser(user.UserType, isAgentUser)),
	}

	if user.LastLoginDate != nil {
		userTraitOptions = append(userTraitOptions, rs.WithLastLogin(*user.LastLoginDate))
	}

	newUserResource, err := rs.NewUserResource(
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

const agentRuntimeUsersSessionPrefix = "agent_runtime_users"

// pagination.Bag phase labels (ResourceTypeID) for List's two phases.
const (
	agentsPaginationPhase = "agents"
	usersPaginationPhase  = "users"
)

// List emits users in two pagination.Bag phases: the agents phase records which
// users back an Einstein Bot / Agentforce Agent in the session store, then the
// users phase emits users and marks any recorded one SERVICE. The agents phase
// runs only when a session store is present.
func (o *userBuilder) List(
	ctx context.Context,
	_ *v2.ResourceId,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Resource,
	*rs.SyncOpResults,
	error,
) {
	ss := attrs.Session
	pageSize := attrs.PageToken.Size

	// Without a session store the agents phase can't run; log once (first page) so
	// the skipped agent classification isn't silent. Debug, not Warn: the local CLI
	// never has a session store, so this fires on every local sync.
	if ss == nil && attrs.PageToken.Token == "" {
		ctxzap.Extract(ctx).Debug("salesforce-connector: no session store; agent runtime users will not be classified")
	}

	bag := &pagination.Bag{}
	if err := bag.Unmarshal(attrs.PageToken.Token); err != nil {
		return nil, nil, err
	}
	if bag.Current() == nil {
		bag.Push(pagination.PageState{ResourceTypeID: usersPaginationPhase})
		if ss != nil {
			bag.Push(pagination.PageState{ResourceTypeID: agentsPaginationPhase})
		}
	}

	switch bag.ResourceTypeID() {
	case agentsPaginationPhase:
		return o.listAgentRuntimeUsersPage(ctx, bag, ss)
	case usersPaginationPhase:
		return o.listUserPage(ctx, bag, ss, pageSize)
	default:
		return nil, nil, fmt.Errorf("baton-salesforce: unexpected user pagination phase %q", bag.ResourceTypeID())
	}
}

// listAgentRuntimeUsersPage reads one BotDefinition page and records its BotUserIDs
// in the session store, then advances the bag to the next page or the users phase.
func (o *userBuilder) listAgentRuntimeUsersPage(
	ctx context.Context,
	bag *pagination.Bag,
	ss sessions.SessionStore,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bots, nextToken, ratelimitData, err := o.client.GetBotDefinitions(ctx, bag.PageToken())
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		// INVALID_TYPE (no Agentforce) already comes back empty + nil, so this is a real failure.
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to read agent runtime users: %w", err)
	}

	agentUsers := make(map[string]bool, len(bots))
	for _, bot := range bots {
		if bot.BotUserID != "" {
			agentUsers[bot.BotUserID] = true
		}
	}
	if err := session.SetManyJSON(ctx, ss, agentUsers, sessions.WithPrefix(agentRuntimeUsersSessionPrefix)); err != nil {
		// Don't continue with a partial set.
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to record agent runtime users: %w", err)
	}

	return o.advanceBag(bag, nextToken, nil, outputAnnotations)
}

// listUserPage emits one page of users, marking any user recorded by the agents
// phase as SERVICE (without a session store, no agent classification).
func (o *userBuilder) listUserPage(
	ctx context.Context,
	bag *pagination.Bag,
	ss sessions.SessionStore,
	pageSize int,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	users, nextToken, usersRL, err := o.client.GetUsers(
		ctx,
		bag.PageToken(),
		pageSize,
		o.syncDeactivatedUsers,
		o.syncNonStandardUsers,
	)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: client.WithRateLimitAnnotations(usersRL)}, fmt.Errorf("baton-salesforce: failed to list users: %w", err)
	}

	userIDs := make([]string, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}
	userLogins, loginsRL, err := o.client.GetUserLoginsByUserIDs(ctx, userIDs)
	outputAnnotations := client.WithRateLimitAnnotations(usersRL, loginsRL)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to list user logins: %w", err)
	}

	// Recorded by the agents phase; empty when there's no session store.
	agentUsers := map[string]bool{}
	if ss != nil {
		agentUsers, err = session.GetManyJSON[bool](ctx, ss, userIDs, sessions.WithPrefix(agentRuntimeUsersSessionPrefix))
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to read agent runtime users: %w", err)
		}
	}

	rv := make([]*v2.Resource, 0, len(users))
	for _, user := range users {
		newResource, err := userResource(
			ctx,
			user,
			userLogins[user.ID],
			o.shouldUseUsernameForEmail,
			agentUsers[user.ID],
		)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
		}

		rv = append(rv, newResource)
	}

	return o.advanceBag(bag, nextToken, rv, outputAnnotations)
}

// advanceBag advances the bag to nextToken (popping the phase when empty),
// marshals it, and returns the page's resources and results.
func (o *userBuilder) advanceBag(
	bag *pagination.Bag,
	nextToken string,
	resources []*v2.Resource,
	outputAnnotations annotations.Annotations,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	if err := bag.Next(nextToken); err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}
	nextPageToken, err := bag.Marshal()
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}
	return resources, &rs.SyncOpResults{NextPageToken: nextPageToken, Annotations: outputAnnotations}, nil
}

// Entitlements always returns an empty slice for users.
func (o *userBuilder) Entitlements(
	_ context.Context,
	resource *v2.Resource,
	_ rs.SyncOpAttrs,
) (
	[]*v2.Entitlement,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (o *userBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Grant,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

func getUserCreateRequestParams(accountInfo *v2.AccountInfo) (*client.UserCreateRequest, error) {
	email, ok := rs.GetProfileStringValue(accountInfo.Profile, "email")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing email in account info")
	}

	alias, ok := rs.GetProfileStringValue(accountInfo.Profile, "alias")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing alias in account info")
	}

	firstName, ok := rs.GetProfileStringValue(accountInfo.Profile, "first_name")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing first_name in account info")
	}

	lastName, ok := rs.GetProfileStringValue(accountInfo.Profile, "last_name")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing last_name in account info")
	}

	profileId, ok := rs.GetProfileStringValue(accountInfo.Profile, "profileId")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing profileId in account info")
	}

	timezone, ok := rs.GetProfileStringValue(accountInfo.Profile, "timezone")
	if !ok {
		return nil, fmt.Errorf("baton-salesforce: missing timezone in account info")
	}

	contactID, _ := rs.GetProfileStringValue(accountInfo.Profile, "contactID")

	extraFields := make(map[string]any)
	for key, val := range accountInfo.Profile.Fields {
		if _, isSchemaField := accountCreationSchema.FieldMap[key]; !isSchemaField {
			switch v := val.GetKind().(type) {
			case *structpb.Value_StringValue:
				extraFields[key] = v.StringValue
			case *structpb.Value_NumberValue:
				extraFields[key] = v.NumberValue
			case *structpb.Value_BoolValue:
				extraFields[key] = v.BoolValue
			case *structpb.Value_NullValue:
				// null carries no meaningful data, skip silently
			default:
				return nil, fmt.Errorf("baton-salesforce: extra field %q has unsupported value type %T", key, val.GetKind())
			}
		}
	}

	return &client.UserCreateRequest{
		Email:       email,
		Alias:       alias,
		TimeZoneSid: timezone,
		ProfileId:   profileId,
		FirstName:   firstName,
		LastName:    lastName,
		ContactID:   contactID,
		ExtraFields: extraFields,
	}, nil
}

func (o *userBuilder) Delete(
	ctx context.Context,
	resourceId *v2.ResourceId,
) (
	annotations.Annotations,
	error,
) {
	l := ctxzap.Extract(ctx)

	userId := resourceId.Resource
	isActive := false

	ratelimitData, err := o.client.SetUserActiveState(ctx, userId, isActive)
	if err != nil {
		l.Error("Failed to update user status",
			zap.String("resource_id", userId),
			zap.Bool("is_active", isActive),
			zap.Error(err))

		return nil, err
	}

	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)

	return outputAnnotations, nil
}

func (o *userBuilder) CreateAccount(
	ctx context.Context,
	accountInfo *v2.AccountInfo,
	_ *v2.LocalCredentialOptions,
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

	user, err := o.client.GetUserByEmailWithRetry(ctx, userRequest.Email)
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

	r, err := userResource(ctx, user, userLogin, o.shouldUseUsernameForEmail, false)
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
	syncDeactivatedUsers bool,
	syncNonStandardUsers bool,
) *userBuilder {
	return &userBuilder{
		resourceType:              resourceTypeUser,
		client:                    client,
		shouldUseUsernameForEmail: shouldUseUsernameForEmail,
		syncDeactivatedUsers:      syncDeactivatedUsers,
		syncNonStandardUsers:      syncNonStandardUsers,
	}
}
