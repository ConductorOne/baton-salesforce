package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/simpleforce"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	LimitsPath         = "/services/data/v64.0/limits"
	ResetPasswordPath  = "/services/data/v64.0/sobjects/User/%s/password"
	PageSizeDefault    = 100
	SalesforceClientID = "ConductorOne"
	GroupIDPrefix      = "00G"
	UserIDPrefix       = "005"

	trueConst = "true"
)

type SalesforceClient struct {
	baseUrl             string
	client              *simpleforce.Client
	salesforceTransport *salesforceHttpTransport
	TokenSource         oauth2.TokenSource
	Username            string
	password            string
	securityToken       string
	initialized         bool
}

// Gathered from the UserType field found here:
// https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_user.htm
var userTypesToSkip = map[string]bool{
	"CspLitePortal":        true,
	"CustomerSuccess":      true,
	"PowerCustomerSuccess": true,
	"CsnOnly":              true,
	"Guest":                true,
}

type salesforceHttpTransport struct {
	base        http.RoundTripper
	rateLimit   *v2.RateLimitDescription
	tokenSource oauth2.TokenSource
}

func New(
	baseUrl string,
	tokenSource oauth2.TokenSource,
	username string,
	password string,
	securityToken string,
) *SalesforceClient {
	return &SalesforceClient{
		baseUrl:       baseUrl,
		password:      password,
		securityToken: securityToken,
		TokenSource:   tokenSource,
		Username:      username,
	}
}

func (c *SalesforceClient) Initialize(ctx context.Context) error {
	logger := ctxzap.Extract(ctx)
	if c.initialized {
		return nil
	}
	logger.Debug("Initializing Salesforce client")

	simpleClient, err := simpleforce.NewClient(
		ctx,
		c.baseUrl,
		SalesforceClientID,
		simpleforce.DefaultAPIVersion,
	)
	if err != nil {
		logger.Error(
			"salesforce-connector: error creating salesforce client",
			zap.Error(err),
		)
		return err
	}

	httpClient, err := uhttp.NewClient(
		ctx,
		uhttp.WithLogger(
			true,
			ctxzap.Extract(ctx),
		),
	)
	if err != nil {
		logger.Error(
			"salesforce-connector: error creating salesforce client",
			zap.Error(err),
		)
		return err
	}
	interceptedTransport := salesforceHttpTransport{
		base:        httpClient.Transport,
		rateLimit:   &v2.RateLimitDescription{},
		tokenSource: c.TokenSource,
	}

	httpClient.Transport = &interceptedTransport
	wrapper, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("creating HTTP wrapper failed: %w", err)
	}

	simpleClient.SetHttpClient(wrapper)

	// OAuth token source takes precedence over username/password.
	if c.TokenSource != nil {
		logger.Debug("Salesforce client using token source")
		token, err := c.TokenSource.Token()
		if err != nil {
			return fmt.Errorf("baton-salesforce: failed to get token: %w", err)
		}
		instanceURL := c.baseUrl
		if v, ok := token.Extra("instance_url").(string); ok && v != "" {
			instanceURL = v
		}
		// SetSidLoc requires a non-empty session ID to mark the client as
		// authenticated. The transport injects the real Bearer token on every
		// request, so the value here is only used to satisfy simpleforce's
		// internal isLoggedIn() check.
		simpleClient.SetSidLoc(token.AccessToken, instanceURL)
	} else {
		logger.Debug("Salesforce client using username and password")
		err = simpleClient.LoginPassword(
			ctx,
			c.Username,
			c.password,
			c.securityToken,
		)
		if err != nil {
			logger.Error("could not login", zap.Error(err))
			return err
		}
	}
	c.client = simpleClient
	c.salesforceTransport = &interceptedTransport
	c.initialized = true
	return nil
}

// Ping verifies that the configured credentials are valid by calling the
// Salesforce limits endpoint. The /limits endpoint is available to any
// authenticated user with API access.
func (c *SalesforceClient) Ping(ctx context.Context) (
	*v2.RateLimitDescription,
	error,
) {
	err := c.Initialize(ctx)
	if err != nil {
		return nil, fmt.Errorf("salesforce-connector: failed to initialize client: %w", err)
	}

	_, err = c.client.ApexREST(
		ctx,
		http.MethodGet,
		LimitsPath,
		nil,
	)
	ratelimitData := c.salesforceTransport.rateLimit
	if err != nil {
		return ratelimitData, fmt.Errorf("salesforce-connector: error validating credentials: %w", err)
	}

	return ratelimitData, nil
}

func getIsActive(record simpleforce.SObject) (bool, error) {
	value := record.InterfaceField("IsActive")
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return v == trueConst, nil
	case int:
		return v == 1, nil
	case int64:
		return v == 1, nil
	case float64:
		return v == 1, nil
	default:
		return false, fmt.Errorf("salesforce-connector: unexpected is active type, %s", value)
	}
}

func getBoolField(record simpleforce.SObject, field string) (bool, error) {
	value := record.InterfaceField(field)
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return v == trueConst, nil
	case int:
		return v == 1, nil
	case int64:
		return v == 1, nil
	case float64:
		return v == 1, nil
	default:
		return false, fmt.Errorf("salesforce-connector: unexpected field %s type, %s", field, value)
	}
}

func shouldSkipSyncingUserType(
	ctx context.Context,
	user simpleforce.SObject,
	syncNonStandardUsers bool,
) bool {
	logger := ctxzap.Extract(ctx)

	userType := user.StringField("UserType")
	// If userType is an empty string, the user did not have a type. This is a
	// bug in Salesforce, so we will skip syncing the user and log an error.
	if userType == "" {
		id := user.StringField(SalesforcePK)
		logger.Error(
			"salesforce-client: user type is empty",
			zap.String("id", id),
		)
		return true
	}

	if syncNonStandardUsers {
		return false
	}

	value, ok := userTypesToSkip[userType]
	return ok && value
}

func (c *SalesforceClient) GetUsers(
	ctx context.Context,
	pageToken string,
	pageSize int,
	syncDeactivatedUsers bool,
	syncNonStandardUsers bool,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	// Build the conditional query based on syncNonStandardUsers
	logger := ctxzap.Extract(ctx)
	var query *SalesforceQuery
	if syncNonStandardUsers {
		logger.Debug("salesforce-client: syncing non-standard users")
		query = NewQuery(TableNameUsers) // No user type filter
	} else {
		// Filter for Standard users only - these are full Salesforce users with standard licenses.
		// Other types like Partner, Portal, or Chatter users have limited access and are excluded.
		// See https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_user.htm
		query = NewQuery(TableNameUsers).WhereEq("UserType", "Standard")
	}
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	users := make([]*SalesforceUser, 0)
	for _, record := range records {
		isActive, err := getIsActive(record)
		if err != nil {
			return nil, "", nil, err
		}
		if !syncDeactivatedUsers && !isActive {
			continue
		}
		if shouldSkipSyncingUserType(ctx, record, syncNonStandardUsers) {
			continue
		}

		lastLogin, err := parseSalesforceDatetime(record.StringField("LastLoginDate"))
		if err != nil {
			return nil, "", nil, err
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
	return users, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) GetProfileById(ctx context.Context, id string) (*SalesforceProfile, *v2.RateLimitDescription, error) {
	query := NewQuery(TableNameProfiles).WhereEq("Id", id)
	records, _, ratelimitData, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, ratelimitData, err
	}
	if len(records) == 0 {
		return nil, ratelimitData, nil
	}
	return &SalesforceProfile{
		ID:            records[0].ID(),
		Name:          records[0].StringField("Name"),
		UserLicenseId: records[0].StringField("UserLicenseId"),
	}, ratelimitData, nil
}

func (c *SalesforceClient) GetProfileByName(ctx context.Context, name string) (*SalesforceProfile, *v2.RateLimitDescription, error) {
	query := NewQuery(TableNameProfiles).WhereEq("Name", name)
	records, _, ratelimitData, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, ratelimitData, err
	}
	if len(records) == 0 {
		return nil, ratelimitData, nil
	}
	return &SalesforceProfile{
		ID:            records[0].ID(),
		Name:          records[0].StringField("Name"),
		UserLicenseId: records[0].StringField("UserLicenseId"),
	}, ratelimitData, nil
}

// GetUserRoles - SELECT Id, Name FROM UserRole.
func (c *SalesforceClient) GetUserRoles(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceRole,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameRoles)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	roles := make([]*SalesforceRole, 0)
	for _, record := range records {
		roles = append(roles, &SalesforceRole{
			ID:   record.ID(),
			Name: record.StringField("Name"),
		})
	}
	return roles, paginationUrl, ratelimitData, nil
}

// getGroupName - "Role groups" (i.e. groups that were created to group the
// users with that role) have no names of their own. Here we need to get the
// Role data in a second query.
func getGroupName(ctx context.Context, record simpleforce.SObject) string {
	var name string
	role := record.SObjectField(ctx, "Name", "Related")
	if role != nil {
		name = role.StringField("Name")
	}
	if name == "" {
		name = record.StringField("Name")
	}
	return name
}

// GetGroups -.
func (c *SalesforceClient) GetGroups(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceGroup,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameGroups)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	groups := make([]*SalesforceGroup, 0)
	for _, record := range records {
		group := &SalesforceGroup{
			ID:            record.ID(),
			Name:          getGroupName(ctx, record),
			RelatedID:     record.StringField("RelatedId"),
			DeveloperName: record.StringField("DeveloperName"),
			Type:          record.StringField("Type"),
		}
		groups = append(groups, group)
	}

	return groups, paginationUrl, ratelimitData, nil
}

func getPermissionSetName(ctx context.Context, record simpleforce.SObject) string {
	var name string
	profile := record.SObjectField(ctx, "Profile", "Profile")
	if profile != nil {
		name = profile.StringField("Name")
	}
	if name == "" {
		name = record.StringField("Name")
	}
	return name
}

// GetPermissionSets - select Id, Name, Label, Type From PermissionSet  and exclude "profile" type
// Some permission sets are roles and have an id to a role id
// query via Select Id, Name From Profile Where Id In (SELECT ProfileId From PermissionSet)
// also there can be multiple permission sets with the same profile id.
func (c *SalesforceClient) GetPermissionSets(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*SalesforcePermission,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNamePermissionsSets)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}

	permissions := make([]*SalesforcePermission, 0)
	for _, record := range records {
		permissionSet := &SalesforcePermission{
			ID:        record.ID(),
			Name:      getPermissionSetName(ctx, record),
			Label:     record.StringField("Label"),
			Type:      record.StringField("Type"),
			ProfileID: record.StringField("ProfileId"),
		}
		permissions = append(permissions, permissionSet)
	}
	return permissions, paginationUrl, ratelimitData, nil
}

// GetProfiles - // SELECT Id, Name FROM Profile.
func (c *SalesforceClient) GetProfiles(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceProfile,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameProfiles)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	profiles := make([]*SalesforceProfile, 0)
	for _, record := range records {
		profiles = append(profiles, &SalesforceProfile{
			ID:            record.ID(),
			Name:          record.StringField("Name"),
			UserLicenseId: record.StringField("UserLicenseId"),
		})
	}
	return profiles, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) GetUserLicenseByID(ctx context.Context, id string) (*SalesforceUserLicense, *v2.RateLimitDescription, error) {
	query := NewQuery(TableNameUserLicenses).WhereEq("Id", id)
	records, _, ratelimitData, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, ratelimitData, err
	}
	if len(records) == 0 {
		return nil, ratelimitData, nil
	}
	return &SalesforceUserLicense{
		ID:   records[0].ID(),
		Name: records[0].StringField("Name"),
	}, ratelimitData, nil
}

// getAssignments DRY up some querying. This could be smarter to handle more cases.
func (c *SalesforceClient) getAssignments(
	ctx context.Context,
	conditionKey string,
	conditionValue string,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameUsers).WhereEq("UserType", "Standard").WhereEq(conditionKey, conditionValue)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	users := make([]*SalesforceUser, 0)
	for _, record := range records {
		users = append(users, &SalesforceUser{
			ID: record.ID(),
		})
	}
	return users, paginationUrl, ratelimitData, nil
}

// GetProfileAssignments - SELECT Id, ProfileId FROM User.
func (c *SalesforceClient) GetProfileAssignments(
	ctx context.Context,
	profileID string,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	return c.getAssignments(ctx, "ProfileId", profileID, pageToken, pageSize)
}

// GetRoleAssignments - SELECT Id, UserRoleId FROM User Where UserRoleId != "".
func (c *SalesforceClient) GetRoleAssignments(
	ctx context.Context,
	userRoleID string,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	return c.getAssignments(ctx, "UserRoleId", userRoleID, pageToken, pageSize)
}

// GetPermissionSetAssignments - Select Id, PermissionSetId, AssigneeId, IsActive FROM PermissionSetAssignment.
func (c *SalesforceClient) GetPermissionSetAssignments(
	ctx context.Context,
	permissionSetID string,
	pageToken string,
	pageSize int,
) (
	[]*PermissionSetAssignment,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNamePermissionAssignments).WhereEq("PermissionSetId", permissionSetID)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	assignments := make([]*PermissionSetAssignment, 0)
	for _, record := range records {
		assignments = append(assignments, &PermissionSetAssignment{
			ID:              record.ID(),
			PermissionSetID: record.StringField("PermissionSetId"),
			UserID:          record.StringField("AssigneeId"),
			// TODO(marcos): Is this a sane way to decide if the permission set
			//  assignment is still active? Should we be using `IsActive`?
			IsActive: record.StringField("AssigneeId") == trueConst,
		})
	}
	return assignments, paginationUrl, ratelimitData, nil
}

func getIsGroup(record simpleforce.SObject) (bool, error) {
	principalID := record.StringField("UserOrGroupId")
	if strings.HasPrefix(principalID, GroupIDPrefix) {
		return true, nil
	}
	if strings.HasPrefix(principalID, UserIDPrefix) {
		return false, nil
	}
	return false, fmt.Errorf("invalid principal id %s", principalID)
}

// GetGroupMemberships - Select Id, GroupId, UserOrGroupId From GroupMember.
func (c *SalesforceClient) GetGroupMemberships(
	ctx context.Context,
	groupID string,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceGroupMembership,
	string,
	*v2.RateLimitDescription,
	error,
) {
	logger := ctxzap.Extract(ctx)
	query := NewQuery(TableNameGroupMemberships).WhereEq("GroupId", groupID)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}

	memberships := make([]*SalesforceGroupMembership, 0)
	for _, record := range records {
		isGroup, err := getIsGroup(record)
		if err != nil {
			logger.Debug(
				"salesforce-client: skipping record",
				zap.Error(err),
			)
			continue
		}

		grant := &SalesforceGroupMembership{
			ID:          record.ID(),
			GroupID:     record.StringField("GroupId"),
			PrincipalID: record.StringField("UserOrGroupId"),
			IsGroup:     isGroup,
		}
		memberships = append(memberships, grant)
	}
	return memberships, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) getGroupMembership(
	ctx context.Context,
	userId string,
	groupID string,
) (
	*simpleforce.SObject,
	*v2.RateLimitDescription,
	error,
) {
	return c.getSObject(
		ctx,
		NewQuery(TableNameGroupMemberships).
			WhereEq("GroupId", groupID).
			WhereEq("UserOrGroupId", userId),
	)
}

func (c *SalesforceClient) getPermissionSetAssignment(
	ctx context.Context,
	userId string,
	permissionSetId string,
) (
	*simpleforce.SObject,
	*v2.RateLimitDescription,
	error,
) {
	return c.getSObject(
		ctx,
		NewQuery(TableNamePermissionAssignments).
			WhereEq("AssigneeId", userId).
			WhereEq("PermissionSetId", permissionSetId),
	)
}

func (c *SalesforceClient) AddUserToGroup(
	ctx context.Context,
	userId string,
	groupId string,
) (*v2.RateLimitDescription, error) {
	logger := ctxzap.Extract(ctx)
	logger.Debug(
		"add-user-to-group",
		zap.String("user_id", userId),
		zap.String("group_id", groupId),
	)

	return c.CreateObject(
		ctx,
		TableNameGroupMemberships,
		map[string]interface{}{
			"GroupId":       groupId,
			"UserOrGroupId": userId,
		},
	)
}

func (c *SalesforceClient) RemoveUserFromGroup(
	ctx context.Context,
	userId string,
	groupId string,
) (bool, *v2.RateLimitDescription, error) {
	found, ratelimitData, err := c.getGroupMembership(ctx, userId, groupId)
	if err != nil {
		if errors.Is(err, ErrObjectNotFound) {
			return false, ratelimitData, nil
		}
		return false, ratelimitData, err
	}

	ratelimitData, err = c.DeleteObject(ctx, TableNameGroupMemberships, found.ID())
	return true, ratelimitData, err
}

func (c *SalesforceClient) AddUserToPermissionSet(
	ctx context.Context,
	userId string,
	permissionSetId string,
) (*v2.RateLimitDescription, error) {
	return c.CreateObject(
		ctx,
		TableNamePermissionAssignments,
		map[string]interface{}{
			"AssigneeId":      userId,
			"PermissionSetId": permissionSetId,
		},
	)
}

func (c *SalesforceClient) RemoveUserFromPermissionSet(
	ctx context.Context,
	userId string,
	permissionSetId string,
) (*v2.RateLimitDescription, error) {
	found, ratelimitData, err := c.getPermissionSetAssignment(ctx, userId, permissionSetId)
	if err != nil {
		return ratelimitData, err
	}
	return c.DeleteObject(ctx, TableNamePermissionAssignments, found.ID())
}

func (c *SalesforceClient) AddUserToProfile(
	ctx context.Context,
	userId string,
	profileId string,
) (*v2.RateLimitDescription, error) {
	return c.setOneValue(ctx, userId, "ProfileId", profileId)
}

func (c *SalesforceClient) SetNewUserProfile(
	ctx context.Context,
	userId string,
	profileId string,
) (*v2.RateLimitDescription, error) {
	return c.setOneValue(ctx, userId, "ProfileId", profileId)
}

func (c *SalesforceClient) AddUserToRole(
	ctx context.Context,
	userId string,
	roleId string,
) (*v2.RateLimitDescription, error) {
	return c.setValue(ctx, userId, "UserRoleId", roleId)
}

func (c *SalesforceClient) RemoveUserFromRole(
	ctx context.Context,
	userId string,
	roleId string,
) (*v2.RateLimitDescription, error) {
	return c.clearValue(ctx, userId, "UserRoleId", roleId)
}

func (c *SalesforceClient) SetUserActiveState(
	ctx context.Context,
	userId string,
	active bool,
) (*v2.RateLimitDescription, error) {
	rld, err := c.setOneValue(ctx, userId, "IsActive", strconv.FormatBool(active))
	if err != nil {
		return rld, err
	}

	return rld, nil
}

func (c *SalesforceClient) GetPermissionSetGroups(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*PermissionSetGroup,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TablePermissionSetGroup)

	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	permissionSetGroups := make([]*PermissionSetGroup, 0)
	for _, record := range records {
		hasActivationRequired, err := getBoolField(record, "HasActivationRequired")
		if err != nil {
			return nil, "", ratelimitData, err
		}

		isDeleted, err := getBoolField(record, "IsDeleted")
		if err != nil {
			return nil, "", ratelimitData, err
		}

		permissionSetGroups = append(permissionSetGroups, &PermissionSetGroup{
			ID:                    record.ID(),
			Description:           record.StringField("Description"),
			DeveloperName:         record.StringField("DeveloperName"),
			HasActivationRequired: hasActivationRequired,
			IsDeleted:             isDeleted,
			Language:              record.StringField("Language"),
			MasterLabel:           record.StringField("MasterLabel"),
			NamespacePrefix:       record.StringField("NamespacePrefix"),
		})
	}
	return permissionSetGroups, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) GetPermissionSetGroupComponentsByPermissionSet(
	ctx context.Context,
	permissionSetId string,
	pageToken string,
	pageSize int,
) (
	[]*PermissionSetGroupComponent,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TablePermissionSetGroupComponent).
		WhereEq("PermissionSetId", permissionSetId)

	records, paginationUrl, ratelimitData, err := c.query(ctx, query, pageToken, pageSize)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	permissionSetGroupComponents := make([]*PermissionSetGroupComponent, 0)
	for _, record := range records {
		isDeleted, err := getBoolField(record, "IsDeleted")
		if err != nil {
			return nil, "", ratelimitData, err
		}
		permissionSetGroupComponents = append(permissionSetGroupComponents, &PermissionSetGroupComponent{
			ID:                   record.ID(),
			IsDeleted:            isDeleted,
			PermissionSetGroupID: record.StringField("PermissionSetGroupId"),
			PermissionSetID:      record.StringField("PermissionSetId"),
		})
	}
	return permissionSetGroupComponents, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) GetPermissionSetGroupAssignments(
	ctx context.Context,
	permissionSetGroupId string,
	pageToken string,
	pageSize int,
) (
	[]*PermissionSetAssignment,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNamePermissionAssignments, "PermissionSetGroupId", "AssigneeId").
		WhereEq("PermissionSetGroupId", permissionSetGroupId)

	records, paginationUrl, ratelimitData, err := c.query(ctx, query, pageToken, pageSize)
	if err != nil {
		return nil, "", ratelimitData, err
	}

	assignments := make([]*PermissionSetAssignment, 0)
	for _, record := range records {
		assignments = append(assignments, &PermissionSetAssignment{
			ID:                   record.ID(),
			PermissionSetGroupID: record.StringField("PermissionSetGroupId"),
			UserID:               record.StringField("AssigneeId"),
		})
	}
	return assignments, paginationUrl, ratelimitData, nil
}

func (c *SalesforceClient) GetOnePermissionSetGroupAssignment(
	ctx context.Context,
	userId string,
	permissionSetGroupId string,
) (
	*PermissionSetAssignment,
	error,
) {
	query := NewQuery(TableNamePermissionAssignments, "PermissionSetGroupId", "AssigneeId").
		WhereEq("AssigneeId", userId).
		WhereEq("PermissionSetGroupId", permissionSetGroupId)

	records, _, _, err := c.query(ctx, query, "", 1)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}

	return &PermissionSetAssignment{
		ID:                   records[0].ID(),
		PermissionSetGroupID: records[0].StringField("PermissionSetGroupId"),
		UserID:               records[0].StringField("AssigneeId"),
	}, nil
}

func (c *SalesforceClient) AddUserToPermissionSetGroup(
	ctx context.Context,
	userId string,
	permissionSetGroupId string,
) (*v2.RateLimitDescription, error) {
	return c.CreateObject(
		ctx,
		TableNamePermissionAssignments,
		map[string]interface{}{
			"AssigneeId":           userId,
			"PermissionSetGroupId": permissionSetGroupId,
		},
	)
}

func (c *SalesforceClient) RemoveUserFromPermissionSetGroup(
	ctx context.Context,
	assignmentID string,
) (*v2.RateLimitDescription, error) {
	return c.DeleteObject(ctx, TableNamePermissionAssignments, assignmentID)
}

func (c *SalesforceClient) GetConnectedApplications(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*ConnectedApplication,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameConnectedApps)
	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
	if err != nil {
		return nil, "", ratelimitData, err
	}

	apps := make([]*ConnectedApplication, 0)

	for _, record := range records {
		permissionSet := &ConnectedApplication{
			ID:   record.ID(),
			Name: record.StringField("Name"),
		}
		apps = append(apps, permissionSet)
	}
	return apps, paginationUrl, ratelimitData, nil
}

// AgentforceAPIVersion is the REST API version used for BotDefinition queries.
// BotDefinition (Einstein Bots and Agentforce Agents) is GA in API v60.0; the
// shared client is pinned to an older default, so this query opts into v60.0.
const AgentforceAPIVersion = "60.0"

// GetBotDefinitions lists Agentforce agents and Einstein Bots from the
// BotDefinition SObject. Most orgs don't have Agentforce or Einstein Bots
// enabled, in which case the SObject doesn't exist and Salesforce returns
// INVALID_TYPE; that case is treated as "no agents" rather than a sync failure.
func (c *SalesforceClient) GetBotDefinitions(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*BotDefinition,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameBotDefinition)
	records, paginationUrl, ratelimitData, err := c.queryWithAPIVersion(
		ctx,
		query,
		pageToken,
		AgentforceAPIVersion,
	)
	if err != nil {
		if isSObjectNotSupportedError(err) {
			ctxzap.Extract(ctx).Info(
				"salesforce-client: BotDefinition SObject not available; skipping agent sync (Agentforce/Einstein Bots not enabled)",
				zap.Error(err),
			)
			return []*BotDefinition{}, "", ratelimitData, nil
		}
		return nil, "", ratelimitData, err
	}

	agents := make([]*BotDefinition, 0, len(records))
	for _, record := range records {
		agents = append(agents, &BotDefinition{
			ID:            record.ID(),
			DeveloperName: record.StringField("DeveloperName"),
			MasterLabel:   record.StringField("MasterLabel"),
			BotUserId:     record.StringField("BotUserId"),
		})
	}
	return agents, paginationUrl, ratelimitData, nil
}

// userLoginInClauseChunkSize bounds the number of UserIds packed into a single
// WHERE UserId IN (...) clause. Salesforce's GET /query endpoint enforces a
// URL length limit (~16 KB); at ~28 URL-encoded chars per ID, 250 keeps the
// request well under the limit with generous headroom. Safe to raise once
// the path is proven out on a large tenant.
const userLoginInClauseChunkSize = 250

// GetUserLoginsByUserIDs fetches UserLogin records for many users using
// WHERE UserId IN (...) and returns them keyed by UserId. Prefer this over
// calling GetUserLogin in a loop: it turns an N+1 into at most
// ceil(len(userIDs) / userLoginInClauseChunkSize) round trips.
//
// Users without a UserLogin record are absent from the returned map; callers
// should treat a missing key as "no UserLogin" (equivalent to GetUserLogin
// returning nil).
func (c *SalesforceClient) GetUserLoginsByUserIDs(
	ctx context.Context,
	userIDs []string,
) (
	map[string]*UserLogin,
	*v2.RateLimitDescription,
	error,
) {
	logger := ctxzap.Extract(ctx)
	result := make(map[string]*UserLogin, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil, nil
	}

	totalChunks := (len(userIDs) + userLoginInClauseChunkSize - 1) / userLoginInClauseChunkSize

	var ratelimitData *v2.RateLimitDescription
	for start := 0; start < len(userIDs); start += userLoginInClauseChunkSize {
		end := min(start+userLoginInClauseChunkSize, len(userIDs))
		chunk := userIDs[start:end]
		chunkIndex := start/userLoginInClauseChunkSize + 1

		query := NewQuery(TableNameUserLogin)
		inArgs := make([]interface{}, len(chunk))
		for i, v := range chunk {
			inArgs[i] = v
		}
		query.sb.Where(query.sb.In("UserId", inArgs...))
		records, nextPage, rl, err := c.query(ctx, query, "", len(chunk))
		// Carry the most recent rate-limit info forward even on error so the
		// caller can still surface it as an annotation.
		ratelimitData = rl
		if err != nil {
			logger.Debug(
				"salesforce-client: UserLogin chunk failed",
				zap.Int("chunk_index", chunkIndex),
				zap.Int("total_chunks", totalChunks),
				zap.Error(err),
			)
			return nil, ratelimitData, err
		}
		// Guard: this code assumes each IN-clause chunk fits in a single
		// Salesforce query response. Chunk size (250) is well below the REST
		// API's default server-side batch (2000), so pagination should never
		// occur here. If the org's batch size is ever lowered below the
		// chunk size, records past the first page would be silently dropped
		// and frozen users would appear unfrozen — turn that into a loud
		// error instead.
		if nextPage != "" {
			return nil, ratelimitData, fmt.Errorf(
				"baton-salesforce: UserLogin batch query was paginated unexpectedly (chunk size: %d)",
				len(chunk),
			)
		}

		logger.Debug(
			"salesforce-client: UserLogin chunk complete",
			zap.Int("chunk_index", chunkIndex),
			zap.Int("total_chunks", totalChunks),
			zap.Int("chunk_user_count", len(chunk)),
			zap.Int("records_returned", len(records)),
		)

		for _, record := range records {
			isFrozen, err := getBoolField(record, "IsFrozen")
			if err != nil {
				return nil, ratelimitData, err
			}
			isPasswordLocked, err := getBoolField(record, "IsPasswordLocked")
			if err != nil {
				return nil, ratelimitData, err
			}
			userId := record.StringField("UserId")
			if isFrozen {
				logger.Debug(
					"salesforce-client: UserLogin returned IsFrozen=true",
					zap.String("user_id", userId),
					zap.String("user_login_id", record.ID()),
				)
			}
			result[userId] = &UserLogin{
				ID:               record.ID(),
				UserId:           userId,
				IsFrozen:         isFrozen,
				IsPasswordLocked: isPasswordLocked,
			}
		}
	}
	return result, ratelimitData, nil
}

func (c *SalesforceClient) GetUserLogin(
	ctx context.Context,
	userId string,
) (
	*UserLogin,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameUserLogin).WhereEq("UserId", userId)
	records, _, ratelimitData, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, ratelimitData, err
	}

	if len(records) == 0 {
		return nil, ratelimitData, nil
	}

	record := records[0]

	isFrozen, err := getBoolField(record, "IsFrozen")
	if err != nil {
		return nil, nil, err
	}

	isPasswordLocked, err := getBoolField(record, "IsPasswordLocked")
	if err != nil {
		return nil, nil, err
	}

	userLogin := &UserLogin{
		ID:               record.ID(),
		UserId:           record.StringField("UserId"),
		IsFrozen:         isFrozen,
		IsPasswordLocked: isPasswordLocked,
	}
	return userLogin, ratelimitData, nil
}

func (c *SalesforceClient) GetTerritories(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]simpleforce.SObject,
	string,
	*v2.RateLimitDescription,
	error,
) {
	q := NewQuery(TableNameTerritory2).WhereInSubQuery(
		"Territory2ModelId",
		NewIDQuery(TableNameTerritory2Model).WhereEq("State", "Active"),
	)
	records, paginationURL, ratelimitData, err := c.query(ctx, q, pageToken, pageSize)
	if err != nil {
		return nil, "", ratelimitData, err
	}
	return records, paginationURL, ratelimitData, nil
}

func (c *SalesforceClient) GetTerritoryMembers(
	ctx context.Context,
	territoryID string,
	pageToken string,
	pageSize int,
) (
	[]*UserTerritory2Association,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameUserTerritory2Assoc).WhereEq("Territory2Id", territoryID)
	records, paginationURL, ratelimitData, err := c.query(ctx, query, pageToken, pageSize)
	if err != nil {
		return nil, "", ratelimitData, err
	}

	members := make([]*UserTerritory2Association, 0, len(records))
	for _, record := range records {
		members = append(members, &UserTerritory2Association{
			ID:               record.ID(),
			UserId:           record.StringField("UserId"),
			Territory2Id:     record.StringField("Territory2Id"),
			RoleInTerritory2: record.StringField("RoleInTerritory2"),
		})
	}
	return members, paginationURL, ratelimitData, nil
}

// GetTerritoryRoles fetches all active picklist values for RoleInTerritory2 via
// PicklistValueInfo.
func (c *SalesforceClient) GetTerritoryRoles(ctx context.Context) ([]string, *v2.RateLimitDescription, error) {
	records, _, ratelimitData, err := c.query(
		ctx,
		NewQuery(TableNamePicklistValueInfo, "Value").
			WhereEq("EntityParticle.EntityDefinition.QualifiedApiName", TableNameUserTerritory2Assoc).
			WhereEq("EntityParticle.DeveloperName", "RoleInTerritory2").
			WhereBoolEq("IsActive", true).
			WithoutOrderBy(),
		"",
		-1,
	)
	if err != nil {
		return nil, ratelimitData, fmt.Errorf("baton-salesforce: failed to get territory roles: %w", err)
	}

	roles := make([]string, 0, len(records))
	for _, record := range records {
		if value := record.StringField("Value"); value != "" {
			roles = append(roles, value)
		}
	}
	return roles, ratelimitData, nil
}

func (c *SalesforceClient) GetUserTerritoryAssociation(
	ctx context.Context,
	userID string,
	territoryID string,
) (*simpleforce.SObject, *v2.RateLimitDescription, error) {
	return c.getSObject(
		ctx,
		NewQuery(TableNameUserTerritory2Assoc).
			WhereEq("Territory2Id", territoryID).
			WhereEq("UserId", userID),
	)
}

func (c *SalesforceClient) AddUserToTerritory(
	ctx context.Context,
	userID string,
	territoryID string,
) (*v2.RateLimitDescription, error) {
	ratelimitData, err := c.CreateObject(
		ctx,
		TableNameUserTerritory2Assoc,
		map[string]interface{}{
			"UserId":       userID,
			"Territory2Id": territoryID,
		},
	)
	if err != nil {
		if isSalesforceDuplicateError(err) {
			return ratelimitData, ErrObjectAlreadyExists
		}
		return ratelimitData, err
	}
	return ratelimitData, nil
}

func (c *SalesforceClient) RemoveUserFromTerritory(
	ctx context.Context,
	userID string,
	territoryID string,
) (*v2.RateLimitDescription, error) {
	record, ratelimitData, err := c.getSObject(
		ctx,
		NewQuery(TableNameUserTerritory2Assoc).
			WhereEq("Territory2Id", territoryID).
			WhereEq("UserId", userID),
	)
	if err != nil {
		return ratelimitData, err
	}
	return c.DeleteObject(ctx, TableNameUserTerritory2Assoc, record.ID())
}

func (c *SalesforceClient) AddUserToTerritoryWithRole(
	ctx context.Context,
	userID string,
	territoryID string,
	role string,
) (*v2.RateLimitDescription, error) {
	ratelimitData, err := c.CreateObject(ctx, TableNameUserTerritory2Assoc, map[string]interface{}{
		"UserId":           userID,
		"Territory2Id":     territoryID,
		"RoleInTerritory2": role,
	})
	if err != nil {
		if isSalesforceDuplicateError(err) {
			return ratelimitData, ErrObjectAlreadyExists
		}
		return ratelimitData, err
	}
	return ratelimitData, nil
}

func (c *SalesforceClient) SetUserTerritoryRole(
	ctx context.Context,
	assocID string,
	role string,
) (*v2.RateLimitDescription, error) {
	return c.UpdateObject(ctx, TableNameUserTerritory2Assoc, assocID, map[string]interface{}{
		"RoleInTerritory2": role,
	})
}

func (c *SalesforceClient) ClearUserTerritoryRole(
	ctx context.Context,
	userID string,
	territoryID string,
	expectedRole string,
) (*v2.RateLimitDescription, error) {
	record, ratelimitData, err := c.getSObject(
		ctx,
		NewQuery(TableNameUserTerritory2Assoc).
			WhereEq("Territory2Id", territoryID).
			WhereEq("UserId", userID),
	)
	if err != nil {
		return ratelimitData, err
	}
	currentRole := record.StringField("RoleInTerritory2")
	if currentRole == "" {
		return ratelimitData, ErrRoleAlreadyCleared
	}
	if currentRole != expectedRole {
		ctxzap.Extract(ctx).Warn("baton-salesforce: territory role mismatch; treating as already revoked",
			zap.String("expected_role", expectedRole),
			zap.String("current_role", currentRole),
			zap.String("user_id", userID),
			zap.String("territory_id", territoryID),
		)
		return ratelimitData, ErrRoleMismatch
	}
	return c.UpdateObject(ctx, TableNameUserTerritory2Assoc, record.ID(), map[string]interface{}{
		"RoleInTerritory2": "",
	})
}

// AgentforceAPIVersion is the REST API version used for BotDefinition queries.
// BotDefinition (Einstein Bots and Agentforce Agents) is GA in API v60.0; the
// shared client is pinned to an older default, so this query opts into v60.0.
const AgentforceAPIVersion = "60.0"

// GetAgentRuntimeUserIDs returns the set of User IDs that back an Einstein Bot
// or Agentforce Agent, read from BotDefinition.BotUserId — a queryable reference
// to the User the agent runs as (Object Reference, API v60.0+). These are
// non-human service identities, so user sync classifies them as SERVICE.
//
// The lookup is best-effort and never fails the user sync: orgs without
// Agentforce/Einstein Bots return INVALID_TYPE (treated as "no agents"), and any
// other error is logged and yields an empty set, so affected users simply fall
// back to their UserType-based classification rather than breaking the sync.
func (c *SalesforceClient) GetAgentRuntimeUserIDs(
	ctx context.Context,
) (
	map[string]struct{},
	*v2.RateLimitDescription,
	error,
) {
	logger := ctxzap.Extract(ctx)
	agentUserIDs := make(map[string]struct{})

	var ratelimitData *v2.RateLimitDescription
	pageToken := ""
	for {
		query := NewQuery(TableNameBotDefinition)
		records, nextToken, rl, err := c.queryWithAPIVersion(
			ctx,
			query,
			pageToken,
			AgentforceAPIVersion,
		)
		ratelimitData = rl
		if err != nil {
			if isSObjectNotSupportedError(err) {
				logger.Info(
					"salesforce-client: BotDefinition SObject not available; no agent runtime users to classify (Agentforce/Einstein Bots not enabled)",
				)
				return agentUserIDs, ratelimitData, nil
			}
			logger.Warn(
				"salesforce-client: failed to read BotDefinition.BotUserId; agent runtime users will not be specially classified",
				zap.Error(err),
			)
			return agentUserIDs, ratelimitData, nil
		}

		for _, record := range records {
			if botUserID := record.StringField("BotUserId"); botUserID != "" {
				agentUserIDs[botUserID] = struct{}{}
			}
		}

		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}

	return agentUserIDs, ratelimitData, nil
}
