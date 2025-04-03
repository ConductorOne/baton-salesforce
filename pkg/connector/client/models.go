package client

import "time"

type SalesforceUser struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	Username      string     `json:"username"`
	FirstName     string     `json:"first_name"`
	LastName      string     `json:"last_name"`
	UserType      string     `json:"user_type"`
	IsActive      bool       `json:"is_active"`
	LastLoginDate *time.Time `json:"last_login_date"`
}

type SalesforceCompany struct {
	Name string
}

type Info struct {
	User    *SalesforceUser
	Company *SalesforceCompany
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
	ID   string
	Name string
}

type ChatterUser struct {
	CompanyName string `json:"companyName"`
	Email       string `json:"email"`
	ID          string `json:"id"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
}

type PermissionSetAssignment struct {
	ID              string
	UserID          string
	PermissionSetID string
	IsActive        bool
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
