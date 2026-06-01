package connector

import (
	"context"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type agentBuilder struct {
	client *client.SalesforceClient
}

func (o *agentBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return resourceTypeAgent
}

func agentResource(_ context.Context, agent *client.BotDefinition) (*v2.Resource, error) {
	// MasterLabel is the human-facing name; fall back to the API DeveloperName.
	name := agent.MasterLabel
	if name == "" {
		name = agent.DeveloperName
	}

	profile := map[string]interface{}{
		"id":             agent.ID,
		"developer_name": agent.DeveloperName,
		"master_label":   agent.MasterLabel,
	}

	// AgentTrait status and identity_resource_id are left unset: BotDefinition
	// has no confirmed queryable status field (activation status lives on
	// BotVersion) and no confirmed runtime-user reference. This v1 syncer is
	// discovery-only.
	return rs.NewResource(
		name,
		resourceTypeAgent,
		agent.ID,
		rs.WithAgentTrait(
			rs.WithAgentProfile(profile),
		),
	)
}

func (o *agentBuilder) List(
	ctx context.Context,
	parentResourceID *v2.ResourceId,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Resource,
	*rs.SyncOpResults,
	error,
) {
	token := &attrs.PageToken
	agents, nextToken, ratelimitData, err := o.client.GetBotDefinitions(
		ctx,
		token.Token,
		token.Size,
	)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
	}

	rv := make([]*v2.Resource, 0, len(agents))
	for _, agent := range agents {
		newResource, err := agentResource(ctx, agent)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, err
		}

		rv = append(rv, newResource)
	}
	return rv, &rs.SyncOpResults{
		NextPageToken: nextToken,
		Annotations:   outputAnnotations,
	}, nil
}

func (o *agentBuilder) Entitlements(
	ctx context.Context,
	resource *v2.Resource,
	_ rs.SyncOpAttrs,
) (
	[]*v2.Entitlement,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

func (o *agentBuilder) Grants(
	ctx context.Context,
	resource *v2.Resource,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Grant,
	*rs.SyncOpResults,
	error,
) {
	return nil, nil, nil
}

func newAgentBuilder(client *client.SalesforceClient) *agentBuilder {
	return &agentBuilder{
		client: client,
	}
}
