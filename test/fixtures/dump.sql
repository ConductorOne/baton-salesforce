CREATE TABLE GroupMember (
    Id BIGSERIAL PRIMARY KEY,
    GroupId TEXT,
    UserOrGroupId TEXT
);

CREATE TABLE "Group" (
    Id BIGSERIAL PRIMARY KEY,
    Name TEXT,
    RelatedId TEXT,
    DeveloperName TEXT,
    Type TEXT,
    "Related.Name" TEXT
);

CREATE TABLE PermissionSetAssignment (
    Id BIGSERIAL PRIMARY KEY,
    PermissionSetId TEXT,
    AssigneeId TEXT,
    IsActive INTEGER
);

CREATE TABLE PermissionSet (
    Id BIGSERIAL PRIMARY KEY,
    Name TEXT,
    Label TEXT,
    Type TEXT,
    ProfileId TEXT,
    "Profile.Name" TEXT
);

CREATE TABLE Profile (
    Id BIGSERIAL PRIMARY KEY,
    Name TEXT
);

CREATE TABLE UserRole (
    Id BIGSERIAL PRIMARY KEY,
    Name TEXT
);

CREATE TABLE User (
    Id BIGSERIAL PRIMARY KEY,
    FirstName TEXT,
    LastName TEXT,
    Email TEXT,
    Username TEXT,
    IsActive INT,
    UserType TEXT
);


INSERT INTO User (
    FirstName,
    LastName,
    Email,
    Username,
    IsActive,
    UserType
) VALUES (
    'FirstName',
    'LastName',
    'Email',
    'Username',
    1,
    'UserType'
), (
    'FirstName',
    'LastName',
    'Email',
    'Username',
    1,
    'UserType'
), (
    'FirstName',
    'LastName',
    'Email',
    'Username',
    1,
    'UserType'
 );

INSERT INTO Group (
    Name,
    RelatedId,
    DeveloperName,
    Type,
    "Related.Name"
)
VALUES (
    'name',
    '1',
    'developer name',
    'type',
    'related name'
), (
    '',
    '',
    'AllInternalUsers',
    'Organization',
    ''
);

INSERT INTO GroupMember (GroupId, UserOrGroupId) VALUES ('1','1');
INSERT INTO PermissionSet (Name, Label, Type, ProfileId, "Profile.Name") VALUES ('name', 'label', 'type', '1', 'profile name');
INSERT INTO PermissionSetAssignment (PermissionSetId, AssigneeId, IsActive) VALUES ('1', '1', 1);
INSERT INTO Profile (Name) VALUES ('name');
INSERT INTO UserRole (Name) VALUES ('name');
