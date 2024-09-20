package config

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/test"
	"github.com/conductorone/baton-sdk/pkg/ustrings"
)

func TestConfigs(t *testing.T) {
	test.ExerciseTestCasesFromExpressions(
		t,
		ConfigurationSchema,
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
				"--instance-url 1 --salesforce-password 1",
				false,
				"missing username",
			},
			{
				"--instance-url 1 --salesforce-username 1",
				false,
				"missing password",
			},
			{
				"--instance-url 1 --user-username-for-email --salesforce-username 1 --salesforce-password 1",
				true,
				"all",
			},
		},
	)
}
