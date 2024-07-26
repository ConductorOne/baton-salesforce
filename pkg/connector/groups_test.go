package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-salesforce/test"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TesGroupsList(t *testing.T) {
	ctx := context.Background()

	t.Run("should get groups with pagination", func(t *testing.T) {
		server, err := test.FixturesServer()
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()

		confluenceClient, err := client.NewSalesforceClient(
			ctx,
			server.URL,
			"API Key",
		)
		if err != nil {
			t.Fatal(err)
		}
		c := newGroupBuilder(confluenceClient)

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
