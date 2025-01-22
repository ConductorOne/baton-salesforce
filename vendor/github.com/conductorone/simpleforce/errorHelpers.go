package simpleforce

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"net/http"

	"errors"
)

var (
	// ErrFailure is a generic error if none of the other errors are appropriate.
	ErrFailure = errors.New("general failure")

	// ErrAuthentication is returned when authentication failed.
	ErrAuthentication = errors.New("authentication failure")

	// ErrNoTypeIdClientOrId is returned when the sObject has no type id, client or id.
	ErrNoTypeIdClientOrId = errors.New("sObject has no type id, client or id")

	ErrOidNotFound = errors.New("oid not found")
)

type jsonError []struct {
	Message   string `json:"message"`
	ErrorCode string `json:"errorCode"`
}

type xmlError struct {
	Message   string `xml:"Body>Fault>faultstring"`
	ErrorCode string `xml:"Body>Fault>faultcode"`
}

type SalesforceError struct {
	Message      string
	HttpCode     int
	ErrorCode    string
	ErrorMessage string
}

func (err SalesforceError) Error() string {
	return err.Message
}

// Need to get information out of this package.
func ParseSalesforceError(statusCode int, responseBody []byte) (err error) {
	jsonError := jsonError{}
	err = json.Unmarshal(responseBody, &jsonError)
	if err == nil {
		return SalesforceError{
			Message: fmt.Sprintf(
				logPrefix+" Error. http code: %v Error Message:  %v Error Code: %v",
				statusCode, jsonError[0].Message, jsonError[0].ErrorCode,
			),
			HttpCode:     statusCode,
			ErrorCode:    jsonError[0].ErrorCode,
			ErrorMessage: jsonError[0].Message,
		}
	}

	xmlError := xmlError{}
	err = xml.Unmarshal(responseBody, &xmlError)
	if err == nil {
		return SalesforceError{
			Message: fmt.Sprintf(
				logPrefix+" Error. http code: %v Error Message:  %v Error Code: %v",
				statusCode, xmlError.Message, xmlError.ErrorCode,
			),
			HttpCode:     statusCode,
			ErrorCode:    xmlError.ErrorCode,
			ErrorMessage: xmlError.Message,
		}
	}

	return SalesforceError{
		Message:  string(responseBody),
		HttpCode: statusCode,
	}
}

func parseUhttpError(ctx context.Context, resp *http.Response, errHttp error) error {
	l := ctxzap.Extract(ctx)

	if resp == nil {
		return errHttp
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		l.Error("request failed", zap.Int("status_code", resp.StatusCode))

		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(resp.Body)
		if err != nil {
			return errors.Join(errHttp, err)
		}

		newStr := buf.String()
		theError := errors.Join(errHttp, ParseSalesforceError(resp.StatusCode, buf.Bytes()))

		l.Error("Failed resp.body", zap.String("body", newStr))
		return theError
	}

	return errHttp
}
