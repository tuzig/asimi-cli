package main

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestIntentGherkin(t *testing.T) {
	suite := godog.TestSuite{
		Name: "intent-gherkin",
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			registerEdictChatStepDefs(ctx, t)
			registerContinueStepDefs(ctx, t)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"intent/gherkin"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
