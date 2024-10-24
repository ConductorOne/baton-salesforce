package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/simpleforce"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	InfoPath           = "/services/data/v24.0/chatter/users/me"
	PageSizeDefault    = 100
	SalesforceClientID = "ConductorOne"
	GroupIDPrefix      = "00G"
	UserIDPrefix       = "005"
)

type SalesforceClient struct {
	baseUrl             string
	client              *simpleforce.Client
	salesforceTransport *salesforceHttpTransport
	TokenSource         oauth2.TokenSource
	Username            string
	Password            string
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
	base      http.RoundTripper
	rateLimit *v2.RateLimitDescription
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
		Password:      password,
		securityToken: securityToken,
		TokenSource:   tokenSource,
		Username:      username,
	}
}

func (c *SalesforceClient) Initialize(ctx context.Context) error {
	logger := ctxzap.Extract(ctx)
	if c.initialized {
		logger.Debug("Salesforce client already initialized")
		return nil
	}
	logger.Debug("Initializing Salesforce client")

	simpleClient := simpleforce.NewClient(
		c.baseUrl,
		SalesforceClientID,
		simpleforce.DefaultAPIVersion,
	)

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
		base:      httpClient.Transport,
		rateLimit: &v2.RateLimitDescription{},
	}

	httpClient.Transport = &interceptedTransport
	wrapper, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return fmt.Errorf("creating HTTP wrapper failed: %w", err)
	}

	simpleClient.SetHttpClient(wrapper)

	// Oauth takes precedence over username, password.
	if c.TokenSource != nil {
		logger.Debug("Salesforce client using token source")
		token, err := c.TokenSource.Token()
		if err != nil {
			return err
		}
		simpleClient.SetSidLoc(token.AccessToken, c.baseUrl)
	} else {
		logger.Debug("Salesforce client using username and password")
		err = simpleClient.LoginPassword(
			c.Username,
			c.Password,
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

func (c *SalesforceClient) GetInfo(ctx context.Context) (
	*Info,
	*v2.RateLimitDescription,
	error,
) {
	err := c.Initialize(ctx)
	if err != nil {
		return nil, nil, err
	}

	response, err := c.client.ApexREST(
		http.MethodGet,
		InfoPath,
		nil,
	)
	ratelimitData := c.salesforceTransport.rateLimit
	if err != nil {
		return nil, ratelimitData, fmt.Errorf("error getting info from connectorClient: %w", err)
	}

	chatterUser := &ChatterUser{}
	err = json.Unmarshal(response, chatterUser)
	if err != nil {
		return nil, nil, fmt.Errorf("error decoding chatter user froms connectorClient")
	}

	info := Info{
		User: &SalesforceUser{
			ID:        chatterUser.ID,
			Email:     chatterUser.Email,
			FirstName: chatterUser.FirstName,
			LastName:  chatterUser.LastName,
		},
		Company: &SalesforceCompany{
			Name: chatterUser.CompanyName,
		},
	}
	return &info, ratelimitData, nil
}

func getIsActive(record simpleforce.SObject) (bool, error) {
	value := record.InterfaceField("IsActive")
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return v == "true", nil
	case int, float64:
		return v == 1, nil
	default:
		return false, fmt.Errorf("salesforce-connector: unexpected is active type, %s", value)
	}
}

func shouldSkipSyncingUserType(
	ctx context.Context,
	user simpleforce.SObject,
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

	value, ok := userTypesToSkip[userType]
	return ok && value
}

func (c *SalesforceClient) GetUsers(
	ctx context.Context,
	pageToken string,
	pageSize int,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TableNameUsers)
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
		if shouldSkipSyncingUserType(ctx, record) {
			continue
		}
		users = append(users, &SalesforceUser{
			ID:        record.ID(),
			Username:  record.StringField("Username"),
			Email:     record.StringField("Email"),
			FirstName: record.StringField("FirstName"),
			LastName:  record.StringField("LastName"),
			UserType:  record.StringField("UserType"),
			IsActive:  isActive,
		})
	}
	return users, paginationUrl, ratelimitData, nil
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
func getGroupName(record simpleforce.SObject) string {
	var name string
	role := record.SObjectField("Name", "Related")
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
			Name:          getGroupName(record),
			RelatedID:     record.StringField("RelatedId"),
			DeveloperName: record.StringField("DeveloperName"),
			Type:          record.StringField("Type"),
		}
		groups = append(groups, group)
	}

	return groups, paginationUrl, ratelimitData, nil
}

func getPermissionSetName(record simpleforce.SObject) string {
	var name string
	profile := record.SObjectField("Profile", "Profile")
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
			Name:      getPermissionSetName(record),
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
			ID:   record.ID(),
			Name: record.StringField("Name"),
		})
	}
	return profiles, paginationUrl, ratelimitData, nil
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
	query := NewQuery(TableNameUsers).WhereEq(conditionKey, conditionValue)
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

// GetRoleAssignments - SELECT Id, UserRoleId FROM User Where UserRoleId != ”.
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
			IsActive: record.StringField("AssigneeId") == "true",
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
		if errors.Is(err, ObjectNotFound) {
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
	return c.setValue(ctx, userId, "ProfileId", profileId)
}

func (c *SalesforceClient) RemoveUserFromProfile(
	ctx context.Context,
	userId string,
	profileId string,
) (*v2.RateLimitDescription, error) {
	return c.clearValue(ctx, userId, "ProfileId", profileId)
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
