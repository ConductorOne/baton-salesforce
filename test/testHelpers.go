package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
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

	db, err := sql.Open("ramsql", "TestLoadUserAddresses")
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
	rows, err := db.Query(queryString)
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
				writer.WriteHeader(http.StatusOK)

				queryString := request.URL.Query().Get("q")
				var offset int
				var totalSize int
				var rows []simpleforce.SObject
				if queryString == "" {
					queryString = request.URL.Query().Get("next")
					totalSize, err = strconv.Atoi(request.URL.Query().Get("total"))
					offset, err = strconv.Atoi(request.URL.Query().Get("offset"))
					rows, err = query(db, queryString)
					if err != nil {
						writer.WriteHeader(http.StatusInternalServerError)
						return
					}
				} else {
					tablename := "User"
					parts := strings.Split(queryString, " ")
					for i, part := range parts {
						if part == "FROM" {
							tablename = parts[i+1]
						}
					}

					rows, err = query(db, fmt.Sprintf("select * from %s", tablename))
					if err != nil {
						writer.WriteHeader(http.StatusInternalServerError)
						return
					}
					totalSize = len(rows)
					offset = 0

					// First one is always empty.
					rows = rows[0:0]
				}

				done := len(rows)+offset >= totalSize

				nextRecordsURL := fmt.Sprintf(
					"/services/data?next=%s&total=%d&offset=%d",
					url.QueryEscape(queryString),
					totalSize,
					offset+len(rows),
				)
				if done {
					nextRecordsURL = ""
				}

				output, err := json.Marshal(QueryResult{
					TotalSize:      totalSize,
					Done:           done,
					NextRecordsURL: nextRecordsURL,
					Records:        rows,
				})
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, err = writer.Write(output)
				if err != nil {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
			},
		),
	), nil
}
