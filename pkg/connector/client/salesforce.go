package client

import (
	"context"
	"encoding/json"
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
	InfoPath           = "/services/data/v64.0/chatter/users/me"
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
			ctx,
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
		ctx,
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
		return v == trueConst, nil
	case int, float64:
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
	case int, float64:
		return v == 1, nil
	default:
		return false, fmt.Errorf("salesforce-connector: unexpected field %s type, %s", field, value)
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
	syncDeactivatedUsers bool,
) (
	[]*SalesforceUser,
	string,
	*v2.RateLimitDescription,
	error,
) {
	// Filter for Standard users only - these are full Salesforce users with standard licenses.
	// Other types like Partner, Portal, or Chatter users have limited access and are excluded.
	// See https://developer.salesforce.com/docs/atlas.en-us.object_reference.meta/object_reference/sforce_api_objects_user.htm
	query := NewQuery(TableNameUsers).WhereEq("UserType", "Standard")
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
		if shouldSkipSyncingUserType(ctx, record) {
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

func (c *SalesforceClient) GetPermissionSetGroupComponent(
	ctx context.Context,
	permissionSetGroupId string,
	pageToken string,
	pageSize int,
) (
	[]*PermissionSetGroupComponent,
	string,
	*v2.RateLimitDescription,
	error,
) {
	query := NewQuery(TablePermissionSetGroupComponent).
		WhereEq("PermissionSetGroupId", permissionSetGroupId)

	records, paginationUrl, ratelimitData, err := c.query(
		ctx,
		query,
		pageToken,
		pageSize,
	)
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

func (c *SalesforceClient) GetOnePermissionSetGroupComponent(
	ctx context.Context,
	permissionSetGroupId string,
	permissionSetId string,
) (
	*PermissionSetGroupComponent,
	error,
) {
	query := NewQuery(TablePermissionSetGroupComponent).
		WhereEq("PermissionSetGroupId", permissionSetGroupId).
		WhereEq("PermissionSetId", permissionSetId)

	records, _, _, err := c.query(
		ctx,
		query,
		"",
		1,
	)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, nil
	}

	if len(records) != 1 {
		return nil, fmt.Errorf("expected 1 record, got %d", len(records))
	}

	record := records[0]

	isDeleted, err := getBoolField(record, "IsDeleted")
	if err != nil {
		return nil, err
	}

	permission := &PermissionSetGroupComponent{
		ID:                   record.ID(),
		IsDeleted:            isDeleted,
		PermissionSetGroupID: record.StringField("PermissionSetGroupId"),
		PermissionSetID:      record.StringField("PermissionSetId"),
	}

	return permission, nil
}

func (c *SalesforceClient) CreatePermissionSetGroupComponent(
	ctx context.Context,
	permissionSetGroupId string,
	permissionSetId string,
) (*v2.RateLimitDescription, error) {
	return c.CreateObject(
		ctx,
		TablePermissionSetGroupComponent,
		map[string]interface{}{
			"PermissionSetGroupId": permissionSetGroupId,
			"PermissionSetId":      permissionSetId,
		},
	)
}

func (c *SalesforceClient) DeletePermissionSetGroupComponent(
	ctx context.Context,
	permissionSetGroupComponentId string,
) (*v2.RateLimitDescription, error) {
	return c.DeleteObject(
		ctx,
		TablePermissionSetGroupComponent,
		permissionSetGroupComponentId,
	)
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
