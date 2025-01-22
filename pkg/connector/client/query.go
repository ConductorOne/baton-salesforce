package client

import (
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
	TableNameRoles                   = "UserRole"
	TableNameUsers                   = "User"
	TablePermissionSetGroup          = "PermissionSetGroup"
	TablePermissionSetGroupComponent = "PermissionSetGroupComponent"
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
	},
	TableNameRoles: {
		"Name",
	},
	TableNameProfiles: {
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
}

type SalesforceQuery struct {
	sb *sqlbuilder.SelectBuilder
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

func (q *SalesforceQuery) WhereNotEq(field string, value string) *SalesforceQuery {
	q.sb.Where(q.sb.NE(field, value))
	return q
}

func (q *SalesforceQuery) WhereGT(field string, value string) *SalesforceQuery {
	q.sb.Where(q.sb.GT(field, value))
	return q
}

func (q *SalesforceQuery) WhereInSubQuery(field string, sq *SalesforceQuery) *SalesforceQuery {
	q.sb.Where(q.sb.In(field, sq.String()))
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
