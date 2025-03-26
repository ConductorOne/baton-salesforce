package client

import "time"

func parseSalesforceDatetime(date string) (*time.Time, error) {
	if date == "" {
		return nil, nil
	}

	t, err := time.Parse("2006-01-02T15:04:05.000-0700", date)
	if err != nil {
		return nil, err
	}

	t = t.UTC()

	return &t, nil
}
