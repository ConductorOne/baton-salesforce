package simpleforce

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/conductorone/baton-sdk/pkg/uhttp"
)

const (
	DefaultAPIVersion = "54.0"
	DefaultClientID   = "simpleforce"
	DefaultURL        = "https://login.salesforce.com"

	logPrefix = "[simpleforce]"
)

// Client is the main instance to access salesforce.
type Client struct {
	sessionID string
	user      struct {
		id       string
		name     string
		fullName string
		email    string
	}
	clientID      string
	apiVersion    string
	baseURL       string
	instanceURL   string
	useToolingAPI bool
	httpClient    *uhttp.BaseHttpClient
}

// QueryResult holds the response data from an SOQL query.
type QueryResult struct {
	TotalSize      int       `json:"totalSize"`
	Done           bool      `json:"done"`
	NextRecordsURL string    `json:"nextRecordsUrl"`
	Records        []SObject `json:"records"`
}

// Expose sid to save in admin settings
func (client *Client) GetSid() (sid string) {
	return client.sessionID
}

// Expose Loc to save in admin settings
func (client *Client) GetLoc() (loc string) {
	return client.instanceURL
}

// Set SID and Loc as a means to log in without LoginPassword
func (client *Client) SetSidLoc(sid string, loc string) {
	client.sessionID = sid
	client.instanceURL = loc
}

// Query runs an SOQL query. q could either be the SOQL string or the nextRecordsURL.
func (client *Client) Query(ctx context.Context, q string) (*QueryResult, error) {
	if !client.isLoggedIn() {
		return nil, ErrAuthentication
	}

	var u string
	if strings.HasPrefix(q, "/services/data") {
		// q is nextRecordsURL.
		u = fmt.Sprintf("%s%s", client.instanceURL, q)
	} else {
		// q is SOQL.
		formatString := "%s/services/data/v%s/query?q=%s"
		baseURL := client.instanceURL
		if client.useToolingAPI {
			formatString = strings.Replace(formatString, "query", "tooling/query", -1)
		}
		u = fmt.Sprintf(formatString, baseURL, client.apiVersion, url.QueryEscape(q))
	}

	data, err := client.httpRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	var result QueryResult
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}

	// Reference to client is needed if the object will be further used to do online queries.
	for idx := range result.Records {
		result.Records[idx].setClient(client)
	}

	return &result, nil
}

// ApexREST executes a custom rest request with the provided method, path, and body. The path is relative to the domain.
func (client *Client) ApexREST(ctx context.Context, method, path string, requestBody io.Reader) ([]byte, error) {
	l := ctxzap.Extract(ctx)

	if !client.isLoggedIn() {
		return nil, ErrAuthentication
	}

	u := fmt.Sprintf("%s/%s", client.instanceURL, path)

	data, err := client.httpRequest(ctx, method, u, requestBody)
	if err != nil {
		l.Error("HTTP GET request failed", zap.String("url", u), zap.Error(err))
		return nil, err
	}

	return data, nil
}

// SObject creates an SObject instance with provided type name and associate the SObject with the client.
func (client *Client) SObject(typeName ...string) *SObject {
	obj := &SObject{}
	obj.setClient(client)
	if typeName != nil {
		obj.setType(typeName[0])
	}
	return obj
}

// isLoggedIn returns if the login to salesforce is successful.
func (client *Client) isLoggedIn() bool {
	return client.sessionID != ""
}

// LoginPassword signs into salesforce using password. token is optional if trusted IP is configured.
// Ref: https://developer.salesforce.com/docs/atlas.en-us.214.0.api_rest.meta/api_rest/intro_understanding_username_password_oauth_flow.htm
// Ref: https://developer.salesforce.com/docs/atlas.en-us.214.0.api.meta/api/sforce_api_calls_login.htm
func (client *Client) LoginPassword(ctx context.Context, username, password, token string) error {
	l := ctxzap.Extract(ctx)

	// Use the SOAP interface to acquire session ID with username, password, and token.
	// Do not use REST interface here as REST interface seems to have strong checking against client_id, while the SOAP
	// interface allows a non-exist placeholder client_id to be used.
	soapBody := `<?xml version="1.0" encoding="utf-8" ?>
        <env:Envelope
                xmlns:xsd="http://www.w3.org/2001/XMLSchema"
                xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"
                xmlns:urn="urn:partner.soap.sforce.com">
            <env:Header>
                <urn:CallOptions>
                    <urn:client>%s</urn:client>
                    <urn:defaultNamespace>sf</urn:defaultNamespace>
                </urn:CallOptions>
            </env:Header>
            <env:Body>
                <n1:login xmlns:n1="urn:partner.soap.sforce.com">
                    <n1:username>%s</n1:username>
                    <n1:password>%s%s</n1:password>
                </n1:login>
            </env:Body>
        </env:Envelope>`
	soapBody = fmt.Sprintf(soapBody, client.clientID, username, html.EscapeString(password), token)

	requestUrl := fmt.Sprintf("%s/services/Soap/u/%s", client.baseURL, client.apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestUrl, strings.NewReader(soapBody))
	if err != nil {
		l.Error("error occurred creating request", zap.Error(err))
		return err
	}
	req.Header.Add("Content-Type", "text/xml")
	req.Header.Add("charset", "UTF-8")
	req.Header.Add("SOAPAction", "login")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		l.Error("error occurred submitting request", zap.Error(err))
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		l.Error("request failed", zap.Int("status_code", resp.StatusCode))

		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		newStr := buf.String()
		l.Error("Failed resp.body", zap.String("body", newStr))
		theError := ParseSalesforceError(resp.StatusCode, buf.Bytes())
		return theError
	}

	respData, err := io.ReadAll(resp.Body)

	if err != nil {
		l.Error("error occurred reading response data", zap.Error(err))
	}

	var loginResponse struct {
		XMLName      xml.Name `xml:"Envelope"`
		ServerURL    string   `xml:"Body>loginResponse>result>serverUrl"`
		SessionID    string   `xml:"Body>loginResponse>result>sessionId"`
		UserID       string   `xml:"Body>loginResponse>result>userId"`
		UserEmail    string   `xml:"Body>loginResponse>result>userInfo>userEmail"`
		UserFullName string   `xml:"Body>loginResponse>result>userInfo>userFullName"`
		UserName     string   `xml:"Body>loginResponse>result>userInfo>userName"`
	}

	err = xml.Unmarshal(respData, &loginResponse)
	if err != nil {
		l.Error("error occurred parsing login response", zap.Error(err))
		return err
	}

	// Now we should all be good and the sessionID can be used to talk to salesforce further.
	client.sessionID = loginResponse.SessionID
	client.instanceURL = parseHost(loginResponse.ServerURL)
	client.user.id = loginResponse.UserID
	client.user.name = loginResponse.UserName
	client.user.email = loginResponse.UserEmail
	client.user.fullName = loginResponse.UserFullName

	l.Info("User authenticated", zap.String("user", client.user.name))
	return nil
}

// httpRequest executes an HTTP request to the salesforce server and returns the response data in byte buffer.
func (client *Client) httpRequest(ctx context.Context, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", client.sessionID))
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, parseUhttpError(ctx, resp, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseUhttpError(ctx, resp, err)
	}

	return io.ReadAll(resp.Body)
}

// makeURL generates a REST API URL based on baseURL, APIVersion of the client.
func (client *Client) makeURL(req string) string {
	client.apiVersion = strings.Replace(client.apiVersion, "v", "", -1)
	retURL := fmt.Sprintf("%s/services/data/v%s/%s", client.instanceURL, client.apiVersion, req)
	return retURL
}

// NewClient creates a new instance of the client.
func NewClient(ctx context.Context, url, clientID, apiVersion string) (*Client, error) {

	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	uhttpClient, err := uhttp.NewBaseHttpClientWithContext(ctx, httpClient)
	if err != nil {
		return nil, err
	}

	client := &Client{
		apiVersion: apiVersion,
		baseURL:    url,
		clientID:   clientID,
		httpClient: uhttpClient,
	}

	// Remove trailing "/" from base url to prevent "//" when paths are appended
	if strings.HasSuffix(client.baseURL, "/") {
		client.baseURL = client.baseURL[:len(client.baseURL)-1]
	}

	return client, nil
}

func (client *Client) SetHttpClient(c *uhttp.BaseHttpClient) {
	client.httpClient = c
}

// DownloadFile downloads a file based on the REST API path given. Saves to filePath.
func (client *Client) DownloadFile(contentVersionID string, filepath string) error {
	apiPath := fmt.Sprintf("/services/data/v%s/sobjects/ContentVersion/%s/VersionData", client.apiVersion, contentVersionID)
	return client.download(apiPath, filepath)
}

func (client *Client) DownloadAttachment(attachmentId string, filepath string) error {
	apiPath := fmt.Sprintf("/services/data/v%s/sobjects/Attachment/%s/Body", client.apiVersion, attachmentId)
	return client.download(apiPath, filepath)
}

func (client *Client) download(apiPath string, filepath string) error {
	// Get the data
	httpClient := client.httpClient
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", strings.TrimRight(client.instanceURL, "/"), apiPath), nil)
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+client.sessionID)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("ERROR: statuscode: %d, body: %s", resp.StatusCode, buf.String())
	}

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}

func parseHost(input string) string {
	parsed, err := url.Parse(input)
	if err == nil {
		return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	}
	return "Failed to parse URL input"
}

// Get the List of all available objects and their metadata for your organization's data
func (client *Client) DescribeGlobal(ctx context.Context) (*SObjectMeta, error) {
	l := ctxzap.Extract(ctx)

	apiPath := fmt.Sprintf("/services/data/v%s/sobjects", client.apiVersion)
	baseURL := strings.TrimRight(client.baseURL, "/")
	url := fmt.Sprintf("%s%s", baseURL, apiPath) // Get the objects
	httpClient := client.httpClient

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+client.sessionID)
	// resp, err := http.Get(url)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var meta SObjectMeta
	l.Info("status code", zap.Int("status_code", resp.StatusCode), zap.String("url", url))

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Error("Failed to read response body", zap.Error(err))
	}

	err = json.Unmarshal(respData, &meta)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}
