package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestUsersList(t *testing.T) {
	ctx := context.Background()

	t.Run("should get users with pagination", func(t *testing.T) {
		server, db, err := test.FixturesServer()
		if err != nil {
			t.Fatal(err)
		}
		defer test.TearDownDB(db)
		defer server.Close()

		salesforceClient, err := test.Client(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		c := newUserBuilder(salesforceClient, false, true)

		resources := make([]*v2.Resource, 0)
		pToken := pagination.Token{
			Token: "",
			Size:  1,
		}
		for {
			nextResources, nextToken, listAnnotations, err := c.List(ctx, nil, &pToken)
			resources = append(resources, nextResources...)

			require.Nil(t, err)
			test.AssertNoRatelimitAnnotations(t, listAnnotations)
			if nextToken == "" {
				break
			}

			pToken.Token = nextToken
		}

		require.NotNil(t, resources)
		require.Len(t, resources, 3)
		require.NotEmpty(t, resources[0].Id)
	})
}
