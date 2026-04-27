package client

import (
	"fmt"

	"github.com/huandu/go-sqlbuilder"
)

const (
	SalesforcePK                     = "Id"
	allFieldsKeyword                 = "Fields(standard)"
	TableNameGroupMemberships        = "GroupMember"
	TableNameGroups                  = "Group"
	TableNamePermissionAssignments   = "PermissionSetAssignment"
	TableNamePermissionsSets         = "PermissionSet"
	TableNameProfiles                = "Profile"
	TableNameUserLicenses            = "UserLicense"
	TableNameRoles                   = "UserRole"
	TableNameUsers                   = "User"
	TablePermissionSetGroup          = "PermissionSetGroup"
	TablePermissionSetGroupComponent = "PermissionSetGroupComponent"
	TableNameConnectedApps           = "ConnectedApplication"
	TableNameUserLogin               = "UserLogin"
	TableNameTerritory2              = "Territory2"
	TableNameTerritory2Model         = "Territory2Model"
	TableNameUserTerritory2Assoc     = "UserTerritory2Association"
	TableNamePicklistValueInfo       = "PicklistValueInfo"
)

var TableNamesToFieldsMapping = map[string][]string{
	TableNameUsers: {
		"FirstName",
		"LastName",
		"Email",
		"Username",
		"IsActive",
		"UserType",
		"ProfileId",
		"UserRoleId",
		"LastLoginDate",
	},
	TableNameRoles: {
		"Name",
	},
	TableNameProfiles: {
		"Name",
		"UserLicenseId",
	},
	TableNameUserLicenses: {
		"Name",
	},
	TableNamePermissionAssignments: {
		"PermissionSetId",
		"AssigneeId",
		"IsActive",
	},
	TableNameGroupMemberships: {
		"GroupId",
		"UserOrGroupId",
	},
	TableNamePermissionsSets: {
		"Name",
		"Label",
		"Type",
		"ProfileId",
		"Profile.Name",
	},
	TableNameGroups: {
		"Name",
		"DeveloperName",
		"Type",
		"RelatedId",
		"Related.Name",
	},
	TablePermissionSetGroup: {
		"IsDeleted",
		"DeveloperName",
		"Language",
		"MasterLabel",
		"NamespacePrefix",
		"Description",
		"HasActivationRequired",
	},
	TablePermissionSetGroupComponent: {
		"IsDeleted",
		"PermissionSetGroupId",
		"PermissionSetId",
	},
	TableNameConnectedApps: {
		"Name",
	},
	TableNameUserLogin: {
		"UserId",
		"IsFrozen",
		"IsPasswordLocked",
	},
	TableNameTerritory2: {
		"Name",
		"Territory2ModelId",
		"Territory2TypeId",
		"ParentTerritory2Id",
		"Description",
	},
	TableNameTerritory2Model: {
		"Name",
		"State",
	},
	TableNameUserTerritory2Assoc: {
		"UserId",
		"Territory2Id",
		"RoleInTerritory2",
	},
	TableNamePicklistValueInfo: {
		"Value",
	},
}

type SalesforceQuery struct {
	sb          *sqlbuilder.SelectBuilder
	skipOrderBy bool
}

// WithoutOrderBy disables the automatic ORDER BY Id that getQueryString adds.
func (q *SalesforceQuery) WithoutOrderBy() *SalesforceQuery {
	q.skipOrderBy = true
	return q
}

// NewIDQuery creates a query that selects only the Id field.
func NewIDQuery(tableName string) *SalesforceQuery {
	return &SalesforceQuery{
		sb: sqlbuilder.Select(SalesforcePK).From(tableName),
	}
}

func NewQuery(tableName string, selectors ...string) *SalesforceQuery {
	if len(selectors) == 0 {
		selectors = TableNamesToFieldsMapping[tableName]
	}
	if len(selectors) == 1 && selectors[0] == "*" {
		selectors[0] = allFieldsKeyword
	} else {
		selectors = append(selectors, SalesforcePK)
	}
	return &SalesforceQuery{
		sb: sqlbuilder.
			Select(selectors...).
			From(tableName),
	}
}

func (q *SalesforceQuery) WhereEq(field string, value string) *SalesforceQuery {
	q.sb.Where(q.sb.Equal(field, value))
	return q
}

// WhereBoolEq adds a boolean SOQL condition. Only use with hardcoded field names — never with user-supplied input.
func (q *SalesforceQuery) WhereBoolEq(field string, value bool) *SalesforceQuery {
	q.sb.Where(fmt.Sprintf("%s = %t", field, value))
	return q
}

// WhereRaw adds a raw SOQL condition without any escaping. Only use with
// hardcoded strings — never with user-supplied input.
func (q *SalesforceQuery) WhereRaw(condition string) *SalesforceQuery {
	q.sb.Where(condition)
	return q
}

func (q *SalesforceQuery) WhereNotEq(field string, value string) *SalesforceQuery {
	q.sb.Where(q.sb.NE(field, value))
	return q
}

func (q *SalesforceQuery) WhereGT(field string, value string) *SalesforceQuery {
	q.sb.Where(q.sb.GT(field, value))
	return q
}

func (q *SalesforceQuery) WhereInSubQuery(field string, sq *SalesforceQuery) *SalesforceQuery {
	q.sb.Where(fmt.Sprintf("%s IN (%s)", field, sq.String()))
	return q
}

func (q *SalesforceQuery) OrderBy(field string) *SalesforceQuery {
	q.sb.OrderBy(field)
	return q
}

func (q *SalesforceQuery) Limit(limit int) *SalesforceQuery {
	q.sb.Limit(limit)
	return q
}

func (q *SalesforceQuery) String() string {
	query, err := sqlbuilder.MySQL.Interpolate(
		q.sb.Build(),
	)
	if err != nil {
		return "invalid query"
	}
	return query
}
