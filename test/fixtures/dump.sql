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
    Id                   TEXT PRIMARY KEY,
    PermissionSetId      TEXT DEFAULT '',
    PermissionSetGroupId TEXT DEFAULT '',
    AssigneeId           TEXT,
    IsActive             INT DEFAULT 1
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
    Name TEXT,
    UserLicenseId TEXT
);

CREATE TABLE UserRole
(
    Id   TEXT PRIMARY KEY,
    Name TEXT
);

CREATE TABLE User
(
    Id            TEXT PRIMARY KEY,
    FirstName     TEXT,
    LastName      TEXT,
    Email         TEXT,
    Username      TEXT,
    IsActive      INT,
    UserType      TEXT,
    ProfileId     TEXT,
    UserRoleId    TEXT,
    LastLoginDate TEXT
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

CREATE TABLE UserLicense
(
    Id   TEXT PRIMARY KEY,
    Name TEXT
);

CREATE TABLE PermissionSetGroupComponent
(
    Id                   TEXT PRIMARY KEY,
    IsDeleted            TEXT,
    PermissionSetGroupId TEXT,
    PermissionSetId      TEXT
) INSERT INTO User (Id,
                  FirstName,
                  LastName,
                  Email,
                  Username,
                  IsActive,
                  UserType,
                  ProfileId,
                  UserRoleId,
                  LastLoginDate
    )
VALUES ('0051X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'Standard',
        '',
        '',
        '2025-03-26T16:43:31.000+0000'),
       ('0052X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'Standard',
        '',
        '',
        '2025-03-26T16:43:31.000+0000'),
       ('0053X',
        'FirstName',
        'LastName',
        'Email',
        'Username',
        1,
        'Standard',
        '',
        '',
        '2025-03-26T16:43:31.000+0000');

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
INSERT INTO PermissionSetAssignment (Id, PermissionSetId, PermissionSetGroupId, AssigneeId, IsActive)
VALUES ('1X', '345X', '', '0051X', 1),
       ('PSA1X', '', 'PSG1X', '0051X', 1);
INSERT INTO Profile (Id, Name, UserLicenseId)
VALUES ('198X', 'name', '1'),
       ('298X', 'name', '2');
INSERT INTO UserRole (Id, Name)
VALUES ('199X', 'name'),
       ('299X', 'name');
INSERT INTO PermissionSet (Id, Name, Label, Type, ProfileId, "Profile")
VALUES ('PS2X', 'ps2', 'PS2 Label', 'type', '', '');
INSERT INTO PermissionSetGroup (Id, IsDeleted, DeveloperName, Language, MasterLabel, NamespacePrefix, Description, HasActivationRequired)
VALUES ('PSG1X', '', 'TestPSG', 'en_US', 'Test PSG', '', 'Test permission set group', '');
INSERT INTO PermissionSetGroupComponent (Id, IsDeleted, PermissionSetGroupId, PermissionSetId)
VALUES ('PSGC1X', '', 'PSG1X', 'PS2X');

CREATE TABLE ConnectedApplication
(
    ID               TEXT PRIMARY KEY,
    Name             TEXT,
    CreatedDate      TEXT,
    CreatedById      TEXT,
    LastModifiedDate TEXT,
)

CREATE TABLE UserLogin
(
    ID               TEXT PRIMARY KEY,
    UserId           TEXT,
    IsFrozen         TEXT,
    IsPasswordLocked TEXT
)

CREATE TABLE Territory2Model
(
    Id    TEXT PRIMARY KEY,
    Name  TEXT,
    State TEXT
)

CREATE TABLE Territory2
(
    Id                  TEXT PRIMARY KEY,
    Name                TEXT,
    Territory2ModelId   TEXT,
    Territory2TypeId    TEXT,
    ParentTerritory2Id  TEXT,
    Description         TEXT
)

CREATE TABLE UserTerritory2Association
(
    Id                TEXT PRIMARY KEY,
    UserId            TEXT,
    Territory2Id      TEXT,
    RoleInTerritory2  TEXT DEFAULT ''
)

CREATE TABLE PicklistValueInfo
(
    Id       TEXT PRIMARY KEY,
    Value    TEXT,
    IsActive INTEGER
)

CREATE TABLE BotDefinition
(
    Id            TEXT PRIMARY KEY,
    DeveloperName TEXT,
    MasterLabel   TEXT
)

INSERT INTO Territory2Model (Id, Name, State) VALUES ('M1', 'Test Model', 'Active');

INSERT INTO Territory2 (Id, Name, Territory2ModelId, Territory2TypeId, ParentTerritory2Id, Description)
VALUES ('T1', 'Argentina', 'M1', '', '', ''),
       ('T2', 'Brasil',    'M1', '', '', '');

INSERT INTO UserTerritory2Association (Id, UserId, Territory2Id, RoleInTerritory2)
VALUES ('A1', '0051X', 'T1', 'Owner'),
       ('A2', '0052X', 'T1', 'Sales Rep'),
       ('A3', '0051X', 'T2', '');

INSERT INTO PicklistValueInfo (Id, Value, IsActive)
VALUES ('PV1', 'Owner', 1),
       ('PV2', 'Sales Rep', 1);

INSERT INTO BotDefinition (Id, DeveloperName, MasterLabel)
VALUES ('0Xx000000000001', 'Service_Agent', 'Service Agent'),
       ('0Xx000000000002', 'Order_Bot', 'Order Bot');