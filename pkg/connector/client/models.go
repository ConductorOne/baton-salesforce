package client

import "time"

type SalesforceUser struct {
	ID                   string     `json:"id"`
	Email                string     `json:"email"`
	Username             string     `json:"username"`
	FirstName            string     `json:"first_name"`
	LastName             string     `json:"last_name"`
	UserType             string     `json:"user_type"`
	LicenseDefinitionKey string     `json:"license_definition_key"`
	IsActive             bool       `json:"is_active"`
	LastLoginDate        *time.Time `json:"last_login_date"`
}

type SalesforceGroupMembership struct {
	ID          string
	GroupID     string
	PrincipalID string // could be user or group
	IsGroup     bool
}

type SalesforceGroup struct {
	ID            string
	Name          string
	Type          string
	RelatedID     string
	DeveloperName string
}

type SalesforceRole struct {
	ID   string
	Name string
}

type SalesforcePermission struct {
	ID        string
	Name      string
	Label     string
	Type      string
	ProfileID string
}

type SalesforceProfile struct {
	ID            string
	Name          string
	UserLicenseId string
}

type SalesforceUserLicense struct {
	ID            string
	Name          string
	TotalLicenses int64
	UsedLicenses  int64
	Status        string
}

type PermissionSetAssignment struct {
	ID                   string
	UserID               string
	PermissionSetID      string
	PermissionSetGroupID string
	IsActive             bool
}

type PermissionSetGroup struct {
	ID                    string
	IsDeleted             bool
	DeveloperName         string
	Language              string
	MasterLabel           string
	NamespacePrefix       string
	Description           string
	HasActivationRequired bool
}

type PermissionSetGroupComponent struct {
	ID                   string
	IsDeleted            bool
	PermissionSetGroupID string
	PermissionSetID      string
}

type ConnectedApplication struct {
	ID               string
	Name             string
	CreatedDate      string
	CreatedById      string
	LastModifiedDate string
}

type BotDefinition struct {
	ID            string
	DeveloperName string
	MasterLabel   string
	BotUserID     string
}

type UserLogin struct {
	ID               string
	UserId           string
	IsFrozen         bool
	IsPasswordLocked bool
}

type UserTerritory2Association struct {
	ID               string
	UserId           string
	Territory2Id     string
	RoleInTerritory2 string
}
