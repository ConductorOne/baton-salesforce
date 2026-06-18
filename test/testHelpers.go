package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/simpleforce"
	"github.com/google/uuid"
	_ "github.com/proullon/ramsql/driver"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// salesforceAPIError lets mock handlers return a non-200 HTTP response with a
// structured Salesforce error body (e.g. DUPLICATE_VALUE).
type salesforceAPIError struct {
	statusCode int
	body       []byte
}

func (e *salesforceAPIError) Error() string {
	return fmt.Sprintf("salesforce API error (HTTP %d): %s", e.statusCode, e.body)
}

type salesforceResponse struct {
	Id      string `json:"id"`
	Success bool   `json:"success"`
}

func Client(ctx context.Context, baseUrl string) (*client.SalesforceClient, error) {
	salesforceClient := client.New(
		baseUrl,
		MockTokenSource(),
		"",
		"",
		"",
	)
	err := salesforceClient.Initialize(ctx)
	if err != nil {
		return nil, err
	}
	return salesforceClient, nil
}

func MockTokenSource() oauth2.TokenSource {
	return oauth2.StaticTokenSource(
		&oauth2.Token{
			AccessToken: "mock-access-token",
		},
	)
}

func AssertNoRatelimitAnnotations(
	t *testing.T,
	actualAnnotations annotations.Annotations,
) {
	if actualAnnotations != nil && len(actualAnnotations) == 0 {
		return
	}

	for _, annotation := range actualAnnotations {
		var ratelimitDescription v2.RateLimitDescription
		err := annotation.UnmarshalTo(&ratelimitDescription)
		if err != nil {
			continue
		}
		if slices.Contains(
			[]v2.RateLimitDescription_Status{
				v2.RateLimitDescription_STATUS_ERROR,
				v2.RateLimitDescription_STATUS_OVERLIMIT,
			},
			ratelimitDescription.Status,
		) {
			t.Fatal("request was ratelimited, expected not to be ratelimited")
		}
	}
}

func TearDownDB(ctx context.Context, db *sql.DB) {
	for key := range client.TableNamesToFieldsMapping {
		_, err := db.ExecContext(
			ctx,
			fmt.Sprintf("DROP TABLE %s", key),
		)
		if err != nil {
			panic(err)
		}
	}

	err := db.Close()
	if err != nil {
		panic(err)
	}
}

func seedDB(ctx context.Context) (*sql.DB, error) {
	data, _ := os.ReadFile("../../test/fixtures/dump.sql")

	db, err := sql.Open("ramsql", "dump")
	if err != nil {
		return nil, err
	}

	_, err = db.ExecContext(ctx, string(data))
	if err != nil {
		return nil, fmt.Errorf("error in adding SQL data: %w", err)
	}

	return db, nil
}

// resolveSubqueries rewrites "IN (SELECT Id FROM Table WHERE ...)" into
// "IN ('id1', 'id2', ...)" because ramsql does not support subqueries.
func resolveSubqueries(ctx context.Context, db *sql.DB, queryString string) (string, error) {
	re := regexp.MustCompile(`IN \(SELECT Id FROM (\w+)(?: WHERE ([^)]+))?\)`)
	var resolveErr error
	result := re.ReplaceAllStringFunc(queryString, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		table := submatches[1]
		innerQuery := fmt.Sprintf("SELECT Id FROM %s", table)
		if len(submatches) > 2 && submatches[2] != "" {
			innerQuery += " WHERE " + submatches[2]
		}
		rows, err := query(ctx, db, innerQuery)
		if err != nil {
			resolveErr = err
			return match
		}
		if len(rows) == 0 {
			return "IN (NULL)"
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, fmt.Sprintf("'%s'", row.ID()))
		}
		return fmt.Sprintf("IN (%s)", strings.Join(ids, ", "))
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return result, nil
}

func query(ctx context.Context, db *sql.DB, queryString string) ([]simpleforce.SObject, error) {
	// The ramsql backing store has no relationship columns, so drop the nested
	// license field from the User query. NewQuery joins fields with ", " while the
	// SObject GET path (find) joins with ",", so strip both forms. Account-type
	// classification is covered by unit tests (TestAccountTypeForUser), not the fixture.
	hackString := strings.ReplaceAll(queryString, ", Profile.UserLicense.LicenseDefinitionKey", "")
	hackString = strings.ReplaceAll(hackString, ",Profile.UserLicense.LicenseDefinitionKey", "")
	hackString = strings.ReplaceAll(hackString, ".Name", "")
	hackString = strings.ReplaceAll(hackString, "Fields(standard)", "Id,*")

	var err error
	hackString, err = resolveSubqueries(ctx, db, hackString)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, hackString) //nolint:gosec // test-only mock server; query is built from internal fixture data, not user input
	if err != nil {
		return nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	output := make([]simpleforce.SObject, 0)
	for rows.Next() {
		// Create a slice of interface{}'s to represent each column,
		// and a second slice to contain pointers to each item in the columns slice.
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		// Scan the result into the column pointers...
		if err := rows.Scan(columnPointers...); err != nil {
			return nil, err
		}

		// Create our map, and retrieve the value for each column from the pointers slice,
		// storing it in the map with the name of the column as the key.
		m := make(map[string]interface{})
		for i, colName := range cols {
			val := columnPointers[i].(*interface{})
			m[colName] = *val
		}
		output = append(output, m)
	}

	return output, nil
}

// QueryResult holds the response data from an SOQL query.
type QueryResult struct {
	TotalSize      int                   `json:"totalSize"`
	Done           bool                  `json:"done"`
	NextRecordsURL string                `json:"nextRecordsUrl"`
	Records        []simpleforce.SObject `json:"records"`
}

func FixturesServer(ctx context.Context) (*httptest.Server, *sql.DB, error) {
	db, err := seedDB(ctx)
	if err != nil {
		return nil, nil, err
	}

	server := httptest.NewServer(
		http.HandlerFunc(
			func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set(uhttp.ContentType, "application/json")

				path := request.URL.Path
				var output []byte
				switch {
				case strings.Contains(path, "sobjects"):
					switch request.Method {
					case http.MethodGet:
						output, err = handleShow(ctx, db, request)
					case http.MethodPatch:
						output, err = handlePatch(ctx, db, request)
					case http.MethodPost:
						output, err = handleInsert(ctx, db, request)
					case http.MethodDelete:
						err = handleDelete(ctx, db, request)
					}
				case request.Method == http.MethodGet:
					output, err = handleQuery(ctx, db, request)
				default:
					err = fmt.Errorf(
						"unsupported method/route: %s %s",
						request.Method,
						path,
					)
				}

				if err != nil {
					var apiErr *salesforceAPIError
					if errors.As(err, &apiErr) {
						writer.WriteHeader(apiErr.statusCode)
						_, _ = writer.Write(apiErr.body)
						return
					}
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}

				limit := 100
				remaining := 100
				writer.Header().Set(client.RateLimitHeaderKey, fmt.Sprintf(client.RateLimitFmt, limit, remaining))
				writer.WriteHeader(http.StatusOK)

				_, err = writer.Write(output) //nolint:gosec // this is a test helper
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
			},
		),
	)
	return server, db, nil
}

func parsePath(request *http.Request) (string, string) {
	re := regexp.MustCompile(`/services/data/([v0-9.]*)/sobjects/([^//]*)/(.*)`)
	matches := re.FindSubmatch([]byte(request.URL.Path))
	return string(matches[2]), string(matches[3])
}

func getBody(request *http.Request) (map[string]interface{}, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	var requestBody map[string]interface{}
	err = json.Unmarshal(body, &requestBody)
	return requestBody, err
}

func handleDelete(ctx context.Context, db *sql.DB, request *http.Request) error {
	_, err := handleShow(ctx, db, request)
	if err != nil {
		return fmt.Errorf("cannot delere noexisting resource")
	}

	tableName, id := parsePath(request)
	result, err := db.ExecContext( //nolint:gosec // test-only mock server; tableName and id come from internal test routing, not user input
		ctx,
		fmt.Sprintf(
			"DELETE FROM %s WHERE Id = '%s'",
			tableName,
			id,
		),
	)
	if err != nil {
		return err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("resource not found")
	}
	return err
}

func handlePatch(ctx context.Context, db *sql.DB, request *http.Request) ([]byte, error) {
	tableName, id := parsePath(request)
	body, err := getBody(request)
	if err != nil {
		return nil, err
	}
	conditions := make([]string, 0)
	for key, value := range body {
		var valueString string
		switch typedValue := value.(type) {
		case string:
			valueString = typedValue
		case int:
			valueString = strconv.Itoa(typedValue)
		case float64:
			valueString = strconv.FormatFloat(typedValue, 'f', 0, 64)
		default:
			return nil, fmt.Errorf("unknown type: %T", value)
		}

		conditions = append(
			conditions,
			fmt.Sprintf(
				`"%s" = '%s'`,
				strings.ToLower(key),
				valueString,
			),
		)
	}

	conditionsString := strings.Join(conditions, ",")
	_, err = db.ExecContext( //nolint:gosec // test-only mock server; tableName, conditionsString, and id come from internal test routing, not user input
		ctx,
		fmt.Sprintf(
			"UPDATE %s SET %s WHERE Id = '%s'",
			tableName,
			conditionsString,
			id,
		),
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func handleInsert(ctx context.Context, db *sql.DB, request *http.Request) ([]byte, error) {
	tableName, _ := parsePath(request)
	body, err := getBody(request)
	if err != nil {
		return nil, err
	}
	// For UserTerritory2Association the unique constraint is (UserId, Territory2Id).
	// Return DUPLICATE_VALUE matching real Salesforce behavior.
	if tableName == client.TableNameUserTerritory2Assoc {
		userID, _ := body["UserId"].(string)
		territory2ID, _ := body["Territory2Id"].(string)
		rows, err := query(ctx, db, fmt.Sprintf(
			"SELECT Id FROM %s WHERE UserId = '%s' AND Territory2Id = '%s'",
			tableName, userID, territory2ID,
		))
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			errBody, _ := json.Marshal([]map[string]interface{}{
				{
					"errorCode": "DUPLICATE_VALUE",
					"message":   fmt.Sprintf("duplicate value found: %s duplicates value on record with id: %s", tableName, rows[0].ID()),
				},
			})
			return nil, &salesforceAPIError{statusCode: http.StatusBadRequest, body: errBody}
		}
	}

	// For GroupMemberships, short-circuit with success if the row already exists.
	if tableName == client.TableNameGroupMemberships {
		conditions := make([]string, 0)
		for key, value := range body {
			conditions = append(conditions, fmt.Sprintf(`%s = '%s'`, key, value))
		}
		conditionsString := strings.Join(conditions, " AND ")
		rows, err := query(ctx, db, fmt.Sprintf("SELECT Id FROM %s WHERE %s", tableName, conditionsString))
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return json.Marshal(salesforceResponse{Id: rows[0].ID(), Success: true})
		}
	}

	nextId, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}

	columns := []string{"Id"}
	values := []string{fmt.Sprintf(`"%s"`, nextId.String())}
	for key, value := range body {
		columns = append(columns, key)
		switch typedValue := value.(type) {
		case string:
			values = append(values, fmt.Sprintf(`"%s"`, typedValue))
		case int:
			values = append(values, strconv.Itoa(typedValue))
		case float64:
			values = append(values, strconv.FormatFloat(typedValue, 'f', 0, 64))
		default:
			return nil, fmt.Errorf("unknown type: %T", value)
		}
	}

	columnsString := "('" + strings.Join(columns, "','") + "')"
	valuesString := "(" + strings.Join(values, ",") + ")"

	_, err = db.ExecContext(ctx, fmt.Sprintf( //nolint:gosec // test-only mock server; tableName and values come from internal fixture data, not user input
		"INSERT INTO %s %s VALUES %s",
		tableName,
		columnsString,
		valuesString,
	))
	if err != nil {
		return nil, err
	}
	return json.Marshal(
		salesforceResponse{
			Id:      nextId.String(),
			Success: true,
		},
	)
}

func getTotalSize(ctx context.Context, db *sql.DB, queryString string) (int, error) {
	parts := strings.Split(queryString, " ")
	for i, part := range parts {
		// get rid of limit and offset if they exist.
		if part == "LIMIT" || part == "OFFSET" {
			parts[i] = ""
			parts[i+1] = ""
		}
	}
	countQuery := strings.Join(parts, " ")
	rows, err := query(ctx, db, countQuery)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func find(ctx context.Context, db *sql.DB, request *http.Request) (simpleforce.SObject, error) {
	tableName, id := parsePath(request)
	selectors := strings.Join(client.TableNamesToFieldsMapping[tableName], ",")
	sqlString := fmt.Sprintf(
		"SELECT Id,%s FROM %s WHERE Id = '%s'",
		selectors,
		tableName,
		id,
	)

	rows, err := query(ctx, db, sqlString)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected 1 row, got %d", len(rows))
	}

	return rows[0], nil
}

func handleShow(ctx context.Context, db *sql.DB, request *http.Request) ([]byte, error) {
	found, err := find(ctx, db, request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(found)
}

func handleQuery(ctx context.Context, db *sql.DB, request *http.Request) ([]byte, error) {
	queryString := request.URL.Query().Get("q")

	// PicklistValueInfo uses Salesforce-specific nested field references in its WHERE
	// clause (EntityParticle.EntityDefinition.QualifiedApiName, EntityParticle.DeveloperName)
	// that SQLite cannot evaluate. Also omit IsActive filter: ramsql stores integer
	// literals from INSERT as empty strings, so numeric comparisons don't work.
	// Fixture data only contains active values, so filtering is not needed here.
	if strings.Contains(queryString, client.TableNamePicklistValueInfo) {
		queryString = fmt.Sprintf("SELECT Id, Value FROM %s", client.TableNamePicklistValueInfo)
	}
	var offset int
	var totalSize int
	var err error
	if queryString == "" {
		queryString = request.URL.Query().Get("next")
		totalSize, err = strconv.Atoi(request.URL.Query().Get("total"))
		if err != nil {
			return nil, err
		}
		offset, err = strconv.Atoi(request.URL.Query().Get("offset"))
		if err != nil {
			return nil, err
		}
	} else {
		totalSize, err = getTotalSize(ctx, db, queryString)
		if err != nil {
			return nil, err
		}
		offset = 0
	}

	rows, err := query(ctx, db, queryString)
	if err != nil {
		return nil, err
	}

	nextOffset := offset + len(rows)
	done := nextOffset >= totalSize

	nextRecordsURL := fmt.Sprintf(
		"/services/data?next=%s&total=%d&offset=%d",
		url.QueryEscape(queryString),
		totalSize,
		nextOffset,
	)
	if done {
		nextRecordsURL = ""
	}

	return json.Marshal(
		QueryResult{
			TotalSize:      totalSize,
			Done:           done,
			NextRecordsURL: nextRecordsURL,
			Records:        rows,
		},
	)
}

func makeError(needle *anypb.Any, haystack ...proto.Message) error {
	sb := make([]string, 0)
	for _, v := range haystack {
		sb = append(sb, string(v.ProtoReflect().Descriptor().FullName()))
	}
	return fmt.Errorf(
		"error: any '%s' did not match expected types: [%s]",
		needle.TypeUrl,
		strings.Join(sb, ", "),
	)
}

// AnyIsOneOf returns an error if the given needle is not found in the haystack.
func AnyIsOneOf(needle *anypb.Any, haystack ...proto.Message) error {
	for _, v := range haystack {
		if needle.MessageIs(v) {
			return nil
		}
	}
	return makeError(needle, haystack...)
}

// TODO(marcos): Move these helpers to baton-sdk and refactor AssertNoRatelimitAnnotations.
func AssertContainsAnnotation(
	t *testing.T,
	expectedAnnotation proto.Message,
	actualAnnotations annotations.Annotations,
) {
	found, err := UnmarshalFromAnys(expectedAnnotation, actualAnnotations)
	if err != nil {
		t.Fatal(err)
	}

	if !found {
		t.Fatal("expected annotation was not in annotations")
	}
}

func AssertDoesNotContainAnnotation(
	t *testing.T,
	expectedAnnotation proto.Message,
	actualAnnotations annotations.Annotations,
) {
	found, err := UnmarshalFromAnys(expectedAnnotation, actualAnnotations)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected annotation was found in annotations")
	}
}

func UnmarshalFromAnys(needle proto.Message, haystack []*anypb.Any) (bool, error) {
	for _, v := range haystack {
		if v.MessageIs(needle) {
			if err := v.UnmarshalTo(needle); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}
