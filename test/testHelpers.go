package test

import (
	"database/sql"
	"encoding/json"
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

	client "github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/google/uuid"
	_ "github.com/proullon/ramsql/driver"
	"github.com/simpleforce/simpleforce"
)

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

func seedDB() (*sql.DB, error) {
	data0, _ := os.ReadFile("../../test/fixtures/dump.sql")

	db, err := sql.Open("ramsql", "dump")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(string(data0))
	if err != nil {
		return nil, fmt.Errorf("error in adding SQL data: %w", err)
	}

	return db, nil
}

func query(db *sql.DB, queryString string) ([]simpleforce.SObject, error) {
	hackString := strings.ReplaceAll(queryString, ".Name", "")
	hackString = strings.ReplaceAll(hackString, "Fields(standard)", "Id,*")

	rows, err := db.Query(hackString)
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

func FixturesServer() (*httptest.Server, error) {
	db, err := seedDB()
	if err != nil {
		return nil, err
	}

	return httptest.NewServer(
		http.HandlerFunc(
			func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set(uhttp.ContentType, "application/json")

				path := request.URL.Path
				var output []byte
				switch {
				case strings.Contains(path, "sobjects"):
					switch request.Method {
					case http.MethodGet:
						output, err = handleShow(db, request)
					case http.MethodPatch:
						output, err = handlePatch(db, request)
					case http.MethodPost:
						output, err = handleInsert(db, request)
					case http.MethodDelete:
						err = handleDelete(db, request)
					}
				case request.Method == http.MethodGet:
					output, err = handleQuery(db, request)
				default:
					err = fmt.Errorf(
						"unsupported method/route: %s %s",
						request.Method,
						path,
					)
				}

				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}

				limit := 100
				remaining := 100
				writer.Header().Set(client.RateLimitHeaderKey, fmt.Sprintf(client.RateLimitFmt, limit, remaining))
				writer.WriteHeader(http.StatusOK)

				_, err = writer.Write(output)
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
			},
		),
	), nil
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

func handleDelete(db *sql.DB, request *http.Request) error {
	tablename, id := parsePath(request)
	_, err := db.Exec(
		fmt.Sprintf(
			"DELETE FROM %s WHERE Id = '%s'",
			tablename,
			id,
		),
	)
	return err
}

func handlePatch(db *sql.DB, request *http.Request) ([]byte, error) {
	tablename, id := parsePath(request)
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
	_, err = db.Exec(
		fmt.Sprintf(
			"UPDATE %s SET %s WHERE Id = '%s'",
			tablename,
			conditionsString,
			id,
		),
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func handleInsert(db *sql.DB, request *http.Request) ([]byte, error) {
	nextId, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}
	tablename, _ := parsePath(request)
	body, err := getBody(request)
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

	_, err = db.Exec(
		fmt.Sprintf(
			"INSERT INTO %s %s VALUES %s",
			tablename,
			columnsString,
			valuesString,
		),
	)
	if err != nil {
		return nil, err
	}

	type salesforceResponse struct {
		Id      string `json:"id"`
		Success bool   `json:"success"`
	}

	return json.Marshal(
		salesforceResponse{
			Id:      nextId.String(),
			Success: true,
		},
	)
}

func getTotalSize(db *sql.DB, queryString string) (int, error) {
	parts := strings.Split(queryString, " ")
	for i, part := range parts {
		// get rid of limit and offset if they exist.
		if part == "LIMIT" || part == "OFFSET" {
			parts[i] = ""
			parts[i+1] = ""
		}
	}
	countQuery := strings.Join(parts, " ")
	rows, err := query(db, countQuery)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func handleShow(db *sql.DB, request *http.Request) ([]byte, error) {
	tablename, id := parsePath(request)
	selectors := strings.Join(client.TableNamesToFieldsMapping[tablename], ",")
	sqlString := fmt.Sprintf(
		"SELECT Id,%s FROM %s WHERE Id = '%s'",
		selectors,
		tablename,
		id,
	)

	rows, err := query(db, sqlString)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("expected 1 row, got %d", len(rows))
	}

	return json.Marshal(rows[0])
}

func handleQuery(db *sql.DB, request *http.Request) ([]byte, error) {
	queryString := request.URL.Query().Get("q")
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
		totalSize, err = getTotalSize(db, queryString)
		if err != nil {
			return nil, err
		}
		offset = 0
	}

	rows, err := query(db, queryString)
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
