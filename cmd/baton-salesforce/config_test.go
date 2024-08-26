package main

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/test"
	"github.com/conductorone/baton-sdk/pkg/ustrings"
)

func TestConfigs(t *testing.T) {
	test.ExerciseTestCasesFromExpressions(
		t,
		field.NewConfiguration(configurationFields),
		nil,
		ustrings.ParseFlags,
		[]test.TestCaseFromExpression{
			{
				"",
				false,
				"empty",
			},
			{
				"--instance-url 1",
				true,
				"is valid",
			},
			{
				"--instance-url 1 --user-username-for-email --access-token 1",
				true,
				"all",
			},
		},
	)
}
