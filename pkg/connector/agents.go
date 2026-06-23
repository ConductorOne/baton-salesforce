package connector

import (
	"context"
	"fmt"

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
	if agent.BotUserID != "" {
		profile["bot_user_id"] = agent.BotUserID
	}

	agentTraitOptions := []rs.AgentTraitOption{
		rs.WithAgentProfile(profile),
	}

	// BotDefinition.BotUserId is a queryable reference to the User the agent runs
	// as (object reference, API v60.0+). When present, link the agent to that
	// runtime user resource so NHI processing can correlate the two.
	if agent.BotUserID != "" {
		agentTraitOptions = append(agentTraitOptions, rs.WithAgentIdentityResourceID(&v2.ResourceId{
			ResourceType: resourceTypeUser.Id,
			Resource:     agent.BotUserID,
		}))
	}

	// AgentTrait status is left unset: BotDefinition has no queryable status
	// field. Activation status lives on BotVersion (API v63.0+), which would
	// raise this syncer's API-version floor and require a per-agent subquery, so
	// it is intentionally out of scope for this v1 discovery syncer.
	return rs.NewResource(
		name,
		resourceTypeAgent,
		agent.ID,
		rs.WithAgentTrait(agentTraitOptions...),
	)
}

func (o *agentBuilder) List(
	ctx context.Context,
	_ *v2.ResourceId,
	attrs rs.SyncOpAttrs,
) (
	[]*v2.Resource,
	*rs.SyncOpResults,
	error,
) {
	token := &attrs.PageToken
	agents, nextToken, ratelimitData, err := o.client.GetBotDefinitions(ctx, token.Token)
	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)
	if err != nil {
		return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to list agents: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(agents))
	for _, agent := range agents {
		newResource, err := agentResource(ctx, agent)
		if err != nil {
			return nil, &rs.SyncOpResults{Annotations: outputAnnotations}, fmt.Errorf("baton-salesforce: failed to build agent resource: %w", err)
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
