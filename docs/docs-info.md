While developing the connector, please fill out this form. This information is needed to write docs and to help other users set up the connector.

## Connector capabilities

1. What resources does the connector sync?

    - Users (standard and optionally non-standard)
    - Groups
    - Permission sets
    - Roles
    - Profiles
    - Connected apps

2. Can the connector provision any resources? If so, which ones? 

    Yes, Accounts, Groups, Roles, Permission sets and profiles.

## Connector credentials 

1. What credentials or information are needed to set up the connector? (For example, API key, client ID and secret, domain, etc.)

    - **Instance URL**: Your Salesforce domain (e.g., `acme.my.salesforce.com`).
    - **Salesforce Username**: The username for the Salesforce account.
    - **Salesforce Password**: The password for the Salesforce account.
    - **Security Token (optional)**: Only required when logging in from untrusted networks or IP addresses.

2. For each item in the list above: 

   * How does a user create or look up that credential or info? Please include links to (non-gated) documentation, screenshots (of the UI or of gated docs), or a video of the process. 

        *   **Instance URL**: This is the domain of your Salesforce organization. You can find it in your browser's address bar when you are logged into Salesforce (e.g., `https://your-domain.my.salesforce.com`).
        *   **Salesforce Username & Password**: These are the standard login credentials for the Salesforce user account that will be used for the integration.
        *   **Security Token (optional)**: Salesforce requires a security token only when logging in from untrusted networks. If the user's password is changed, Salesforce automatically sends a new security token via email.
        *   [Salesforce Documentation: Reset Your Security Token](https://help.salesforce.com/s/articleView?id=sf.user_security_token.htm&type=5)

   * Does the credential need any specific scopes or permissions? If so, list them here. 

        The user account for the integration needs the **"API Enabled"** permission. Additionally, to ensure it can sync all necessary data, it is recommended to use an account with the **"System Administrator"** profile or a custom profile with equivalent permissions, including:

        *   Read access to Users, Groups, Roles, Profiles, and Permission Sets.
        *   If provisioning is enabled, write access to manage Users and Group memberships.

    * If applicable: Is the list of scopes or permissions different to sync (read) versus provision (read-write)? If so, list the difference here. 

        *   **Sync (Read-only)**: Requires read permissions for Users, Groups, Roles, Profiles, Permission Sets, and Connected Apps.
        *   **Provisioning (Read-write)**: In addition to the read permissions, it requires permissions to:
        *   Create and activate user accounts.
        *   Add and remove users from groups.
