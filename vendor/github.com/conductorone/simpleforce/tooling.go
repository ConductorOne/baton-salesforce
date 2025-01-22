package simpleforce

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"net/http"
	"net/url"
)

// ExecuteAnonymousResult is returned by ExecuteAnonymous function
type ExecuteAnonymousResult struct {
	Line                int         `json:"line"`
	Column              int         `json:"column"`
	Compiled            bool        `json:"compiled"`
	Success             bool        `json:"success"`
	CompileProblem      interface{} `json:"compileProblem"`
	ExceptionStackTrace interface{} `json:"exceptionStackTrace"`
	ExceptionMessage    interface{} `json:"exceptionMessage"`
}

// Tooling is called to specify Tooling API, e.g. client.Tooling().Query(q)
func (client *Client) Tooling() *Client {
	client.useToolingAPI = true
	return client
}

func (client *Client) UnTooling() {
	client.useToolingAPI = false
}

// ExecuteAnonymous executes a body of Apex code
func (client *Client) ExecuteAnonymous(ctx context.Context, apexBody string) (*ExecuteAnonymousResult, error) {
	l := ctxzap.Extract(ctx)

	if !client.isLoggedIn() {
		return nil, ErrAuthentication
	}

	// Create the endpoint
	formatString := "%s/services/data/v%s/tooling/executeAnonymous/?anonymousBody=%s"
	baseURL := client.instanceURL
	endpoint := fmt.Sprintf(formatString, baseURL, client.apiVersion, url.QueryEscape(apexBody))

	data, err := client.httpRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		l.Error("HTTP GET request failed", zap.String("endpoint", endpoint))
		return nil, err
	}

	var result ExecuteAnonymousResult
	err = json.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}
