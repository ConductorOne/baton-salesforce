CREATE TABLE GroupMember
(
    Id            TEXT PRIMARY KEY,
    GroupId       TEXT,
    UserOrGroupId TEXT
);

CREATE TABLE "Group"
(
    Id            TEXT PRIMARY KEY,
    Name          TEXT,
    RelatedId     TEXT,
    DeveloperName TEXT,
    Type          TEXT,
    "Related"     TEXT
);

CREATE TABLE PermissionSetAssignment
(
    Id              TEXT PRIMARY KEY,
    PermissionSetId TEXT,
    AssigneeId      TEXT,
    IsActive        INT DEFAULT 1
);

CREATE TABLE PermissionSet
(
    Id        TEXT PRIMARY KEY,
    Name      TEXT,
    Label     TEXT,
    Type      TEXT,
    ProfileId TEXT,
    "Profile" TEXT
);

CREATE TABLE Profile
(
    Id   TEXT PRIMARY KEY,
    Name TEXT
);

CREATE TABLE UserRole
(
    Id   TEXT PRIMARY KEY,
    Name TEXT
);

CREATE TABLE User
(
    Id         TEXT PRIMARY KEY,
    FirstName  TEXT,
    LastName   TEXT,
    Email      TEXT,
    Username   TEXT,
    IsActive   INT,
    UserType   TEXT,
    ProfileId  TEXT,
    UserRoleId TEXT
);

CREATE TABLE PermissionSetGroup
(
    Id                    TEXT PRIMARY KEY,
    IsDeleted             TEXT,
    DeveloperName         TEXT,
    Language              TEXT,
    MasterLabel           TEXT,
    NamespacePrefix       TEXT,
    Description           TEXT,
    HasActivationRequired TEXT
)

CREATE TABLE PermissionSetGroupComponent
(
    Id                   TEXT PRIMARY KEY,
    IsDeleted            TEXT,
    PermissionSetGroupId TEXT,
    PermissionSetId      TEXT
)

INSERT INTO User (Id,
                  FirstName,
                  LastName,
                  Email,
                  Username,
                  IsActive,
                  UserType,
                  ProfileId,
                  UserRoleId)
VALUES ('0051X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'UserType',
        '',
        ''),
       ('0052X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'UserType',
        '',
        ''),
       ('0053X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'UserType',
        '',
        '');

INSERT INTO Group (Id,
                   Name,
                   RelatedId,
                   DeveloperName,
                   Type,
                   "Related")
VALUES ('00G1X',
        'name',
        '1',
        'developer name',
        'type',
        '{"Name": "related name"}'),
       ('00G2X',
        '',
        '',
        'AllInternalUsers',
        'Organization',
        '');

INSERT INTO GroupMember (Id, GroupId, UserOrGroupId)
VALUES ('1X', '00G1X', '0051X');
INSERT INTO PermissionSet (Id, Name, Label, Type, ProfileId, "Profile")
VALUES ('345X', 'name', 'label', 'type', '1', '{"Name": "profile name"}');
INSERT INTO PermissionSetAssignment (Id, PermissionSetId, AssigneeId, IsActive)
VALUES ('1X', '345X', '0051X', 1);
INSERT INTO Profile (Id, Name)
VALUES ('198X', 'name'),
       ('298X', 'name');
INSERT INTO UserRole (Id, Name)
VALUES ('199X', 'name'),
       ('299X', 'name');
