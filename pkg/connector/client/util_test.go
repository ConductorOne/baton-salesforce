package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseSalesforceDatetime(t *testing.T) {
	validDate := time.Date(2025, 3, 26, 16, 43, 31, 0, time.UTC)

	cases := []struct {
		name     string
		input    string
		expected *time.Time
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid date",
			input:    "2025-03-26T16:43:31.000+0000",
			expected: &validDate,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			datetime, err := parseSalesforceDatetime(c.input)
			require.NoError(t, err)

			if c.expected != nil {
				require.EqualValues(t, c.expected, datetime)
			} else {
				require.Nil(t, datetime)
			}
		})
	}
}
