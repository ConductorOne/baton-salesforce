package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-salesforce/pkg/connector/client"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/structpb"
)

func (s *Salesforce) updateUserStatus(ctx context.Context, args *structpb.Struct) (*structpb.Struct, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	guidField, ok := args.Fields["resource_id"].GetKind().(*structpb.Value_StringValue)
	if !ok {
		return nil, nil, fmt.Errorf("missing resource ID")
	}

	isActiveField, ok := args.Fields["is_active"].GetKind().(*structpb.Value_BoolValue)
	if !ok {
		return nil, nil, fmt.Errorf("missing is_active")
	}

	isActive := isActiveField.BoolValue

	userId := guidField.StringValue

	// update user.isActive state
	ratelimitData, err := s.client.SetUserActiveState(ctx, userId, isActive)
	if err != nil {
		l.Error("Failed to update user status",
			zap.String("resource_id", userId),
			zap.Bool("is_active", isActive),
			zap.Error(err))

		return nil, nil, err
	}

	outputAnnotations := client.WithRateLimitAnnotations(ratelimitData)

	response := structpb.Struct{
		Fields: map[string]*structpb.Value{
			"success": {
				Kind: &structpb.Value_BoolValue{BoolValue: true},
			},
		},
	}

	return &response, outputAnnotations, nil
}
