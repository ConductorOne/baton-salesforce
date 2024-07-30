package client

import (
	"fmt"
	"strings"
)

const (
	SalesforcePK                   = "Id"
	allFieldsKeyword               = "Fields(standard)"
	TableNameGroupMemberships      = "GroupMember"
	TableNameGroups                = "Group"
	TableNamePermissionAssignments = "PermissionSetAssignment"
	TableNamePermissionsSets       = "PermissionSet"
	TableNameProfiles              = "Profile"
	TableNameRoles                 = "UserRole"
	TableNameUsers                 = "User"
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
}

type SalesforceQuery struct {
	tableName    string
	selectors    []string
	where        []string
	orderByField string
	limit        int
}

func NewQuery(tableName string, selectors ...string) *SalesforceQuery {
	if len(selectors) == 0 {
		selectors = TableNamesToFieldsMapping[tableName]
	}
	return &SalesforceQuery{
		selectors: selectors,
		where:     make([]string, 0),
		tableName: tableName,
	}
}

func (q *SalesforceQuery) WhereEq(field string, value string) *SalesforceQuery {
	q.where = append(q.where, fmt.Sprintf("%s = '%s'", field, value))
	return q
}

func (q *SalesforceQuery) WhereNotEq(field string, value string) *SalesforceQuery {
	q.where = append(q.where, fmt.Sprintf("%s != '%s'", field, value))
	return q
}

func (q *SalesforceQuery) WhereGT(field string, value string) *SalesforceQuery {
	q.where = append(q.where, fmt.Sprintf("%s < '%s'", field, value))
	return q
}

func (q *SalesforceQuery) WhereInSubQuery(field string, sq *SalesforceQuery) *SalesforceQuery {
	q.where = append(q.where, fmt.Sprintf("%s IN (%s)", field, sq.String()))
	return q
}

func (q *SalesforceQuery) OrderBy(field string) *SalesforceQuery {
	q.orderByField = field
	return q
}

func (q *SalesforceQuery) Limit(limit int) *SalesforceQuery {
	q.limit = limit
	return q
}

func (q *SalesforceQuery) SelectorsString() string {
	if q.selectors != nil && len(q.selectors) > 0 {
		// Automatically include SalesforcePK.
		selectors := q.selectors
		selectors = append(selectors, SalesforcePK)
		return strings.Join(selectors, ",")
	} else {
		return allFieldsKeyword
	}
}

func (q *SalesforceQuery) String() string {
	sb := strings.Builder{}
	_, _ = sb.WriteString(fmt.Sprintf(
		"SELECT %s FROM %s ",
		q.SelectorsString(),
		q.tableName,
	))
	if len(q.where) > 0 {
		wheres := strings.Join(q.where, " AND ")
		_, _ = sb.WriteString(fmt.Sprintf("WHERE %s ", wheres))
	}
	if q.orderByField != "" {
		_, _ = sb.WriteString(fmt.Sprintf("ORDER BY %s ", q.orderByField))
	}
	if q.limit > 0 {
		_, _ = sb.WriteString(fmt.Sprintf("LIMIT %d", q.limit))
	}
	return sb.String()
}
