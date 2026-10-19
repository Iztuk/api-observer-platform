package crs

import (
	"testing"

	"api-observer/internal/audit"
)

func TestTranslateRule(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestBody,
			},
			{
				Variable: Args,
				Selector: "username",
			},
		},

		Operator: Operator{
			Type:    OperatorStringEq,
			Value:   "admin",
			Negated: false,
		},

		Actions: Actions{
			ID:       "100001",
			Phase:    2,
			Message:  "Admin access detected",
			Severity: SeverityWarning,
			Tags: []string{
				"admin",
				"monitoring",
			},
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	rule := result.Rule

	// Phase 2 should map to request scope.
	if len(rule.Scope) != 1 {
		t.Fatalf(
			"expected 1 scope, got %d",
			len(rule.Scope),
		)
	}

	if rule.Scope[0] != audit.RuleScopeRequest {
		t.Errorf(
			"expected request scope, got %s",
			rule.Scope[0],
		)
	}

	// CRS targets are alternatives, so the rule should
	// use MatchModeAny.
	if rule.Match.Mode != audit.MatchModeAny {
		t.Errorf(
			"expected match mode %s, got %s",
			audit.MatchModeAny,
			rule.Match.Mode,
		)
	}

	// RequestBody => 1 condition
	// Args => body + query => 2 conditions
	if len(rule.Match.Conditions) != 3 {
		t.Fatalf(
			"expected 3 match conditions, got %d",
			len(rule.Match.Conditions),
		)
	}

	expectedTargets := []audit.RuleTarget{
		audit.RuleTargetBody,
		audit.RuleTargetBody,
		audit.RuleTargetQuery,
	}

	for i, expectedTarget := range expectedTargets {
		condition := rule.Match.Conditions[i]

		if condition.Target != expectedTarget {
			t.Errorf(
				"condition %d: expected target %s, got %s",
				i,
				expectedTarget,
				condition.Target,
			)
		}

		if condition.Operator != audit.MatchOperatorStringEqual {
			t.Errorf(
				"condition %d: expected operator %s, got %s",
				i,
				audit.MatchOperatorStringEqual,
				condition.Operator,
			)
		}

		if condition.Value != "admin" {
			t.Errorf(
				"condition %d: expected value %q, got %q",
				i,
				"admin",
				condition.Value,
			)
		}

		if condition.Negated {
			t.Errorf(
				"condition %d: expected negated=false",
				i,
			)
		}
	}

	// ARGS query condition should preserve the selector.
	queryCondition := rule.Match.Conditions[2]

	if queryCondition.Key != "username" {
		t.Errorf(
			"expected ARGS query selector %q, got %q",
			"username",
			queryCondition.Key,
		)
	}

	// Finding metadata.
	if rule.Finding.Title != "Admin access detected" {
		t.Errorf(
			"unexpected finding title: %q",
			rule.Finding.Title,
		)
	}

	if rule.Finding.Message != "Admin access detected" {
		t.Errorf(
			"unexpected finding message: %q",
			rule.Finding.Message,
		)
	}

	if rule.Finding.Severity != "WARNING" {
		t.Errorf(
			"expected severity WARNING, got %q",
			rule.Finding.Severity,
		)
	}

	if len(rule.Finding.Tags) != 2 {
		t.Fatalf(
			"expected 2 tags, got %d",
			len(rule.Finding.Tags),
		)
	}

	if len(result.Warnings) != 0 {
		t.Errorf(
			"expected no warnings, got %v",
			result.Warnings,
		)
	}
}

func TestTranslateBodyLength(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestBodyLength,
			},
		},

		Operator: Operator{
			Type:  OperatorGreaterThan,
			Value: "1024",
		},

		Actions: Actions{
			ID:    "100002",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(result.Rule.Match.Conditions) != 1 {
		t.Fatalf(
			"expected 1 condition, got %d",
			len(result.Rule.Match.Conditions),
		)
	}

	condition := result.Rule.Match.Conditions[0]

	if condition.Target != audit.RuleTargetBodyLength {
		t.Errorf(
			"expected target %s, got %s",
			audit.RuleTargetBodyLength,
			condition.Target,
		)
	}

	if condition.Operator != audit.MatchOperatorGreaterThan {
		t.Errorf(
			"expected operator %s, got %s",
			audit.MatchOperatorGreaterThan,
			condition.Operator,
		)
	}

	if condition.Value != "1024" {
		t.Errorf(
			"expected value %q, got %q",
			"1024",
			condition.Value,
		)
	}
}

func TestTranslateCookies(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestCookies,
				Selector: "session",
			},
			{
				Variable: RequestCookiesNames,
			},
		},

		Operator: Operator{
			Type:  OperatorRegex,
			Value: "admin",
		},

		Actions: Actions{
			ID:    "100003",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(result.Rule.Match.Conditions) != 2 {
		t.Fatalf(
			"expected 2 conditions, got %d",
			len(result.Rule.Match.Conditions),
		)
	}

	cookie := result.Rule.Match.Conditions[0]

	if cookie.Target != audit.RuleTargetCookie {
		t.Errorf(
			"expected cookie target, got %s",
			cookie.Target,
		)
	}

	if cookie.Key != "session" {
		t.Errorf(
			"expected cookie key %q, got %q",
			"session",
			cookie.Key,
		)
	}

	cookieName := result.Rule.Match.Conditions[1]

	if cookieName.Target != audit.RuleTargetCookieName {
		t.Errorf(
			"expected cookie_name target, got %s",
			cookieName.Target,
		)
	}
}

func TestTranslateHeaders(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestHeaders,
				Selector: "User-Agent",
			},
			{
				Variable: RequestHeadersNames,
				Selector: "X-Debug",
			},
		},

		Operator: Operator{
			Type:  OperatorStringEq,
			Value: "admin",
		},

		Actions: Actions{
			ID:    "100004",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(result.Rule.Match.Conditions) != 2 {
		t.Fatalf(
			"expected 2 conditions, got %d",
			len(result.Rule.Match.Conditions),
		)
	}

	first := result.Rule.Match.Conditions[0]

	if first.Target != audit.RuleTargetHeader {
		t.Errorf(
			"expected header target, got %s",
			first.Target,
		)
	}

	if first.Key != "User-Agent" {
		t.Errorf(
			"expected User-Agent, got %q",
			first.Key,
		)
	}

	second := result.Rule.Match.Conditions[1]

	if second.Target != audit.RuleTargetHeader {
		t.Errorf(
			"expected header target, got %s",
			second.Target,
		)
	}

	if second.Key != "X-Debug" {
		t.Errorf(
			"expected X-Debug, got %q",
			second.Key,
		)
	}
}

func TestTranslateUnsupportedTargetProducesWarning(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestBody,
			},
			{
				Variable: XML,
			},
		},

		Operator: Operator{
			Type:  OperatorRegex,
			Value: "admin",
		},

		Actions: Actions{
			ID:    "100005",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	// RequestBody should still translate successfully.
	if len(result.Rule.Match.Conditions) != 1 {
		t.Fatalf(
			"expected 1 translated condition, got %d",
			len(result.Rule.Match.Conditions),
		)
	}

	if result.Rule.Match.Conditions[0].Target != audit.RuleTargetBody {
		t.Errorf(
			"expected body target, got %s",
			result.Rule.Match.Conditions[0].Target,
		)
	}

	// XML should become a warning.
	if len(result.Warnings) != 1 {
		t.Fatalf(
			"expected 1 warning, got %d: %v",
			len(result.Warnings),
			result.Warnings,
		)
	}

	t.Logf(
		"translation warning: %s",
		result.Warnings[0],
	)
}

func TestTranslateNegatedOperator(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestMethod,
			},
		},

		Operator: Operator{
			Type:    OperatorStringEq,
			Value:   "POST",
			Negated: true,
		},

		Actions: Actions{
			ID:    "100006",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(result.Rule.Match.Conditions) != 1 {
		t.Fatalf(
			"expected 1 condition, got %d",
			len(result.Rule.Match.Conditions),
		)
	}

	condition := result.Rule.Match.Conditions[0]

	if !condition.Negated {
		t.Error("expected translated condition to be negated")
	}

	if condition.Target != audit.RuleTargetMethod {
		t.Errorf(
			"expected method target, got %s",
			condition.Target,
		)
	}
}

func TestTranslateResponsePhase(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestBody,
			},
		},

		Operator: Operator{
			Type:  OperatorRegex,
			Value: "error",
		},

		Actions: Actions{
			ID:    "100007",
			Phase: 3,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	if len(result.Rule.Scope) != 1 {
		t.Fatalf(
			"expected 1 scope, got %d",
			len(result.Rule.Scope),
		)
	}

	if result.Rule.Scope[0] != audit.RuleScopeResponse {
		t.Errorf(
			"expected response scope, got %s",
			result.Rule.Scope[0],
		)
	}
}

func TestTranslateChainReference(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestMethod,
			},
		},

		Operator: Operator{
			Type:  OperatorStringEq,
			Value: "POST",
		},

		Actions: Actions{
			ID:    "100008",
			Phase: 2,
			Chain: true,
		},

		ChainedRule: &CRSRule{
			Actions: Actions{
				ID: "100009",
			},
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	expected := "crs-rule-100009"

	if result.Rule.Chain != expected {
		t.Errorf(
			"expected chain %q, got %q",
			expected,
			result.Rule.Chain,
		)
	}
}

func TestTranslateFallbackFindingTitle(t *testing.T) {
	source := CRSRule{
		Targets: []Target{
			{
				Variable: RequestMethod,
			},
		},

		Operator: Operator{
			Type:  OperatorStringEq,
			Value: "GET",
		},

		Actions: Actions{
			ID:    "123456",
			Phase: 2,
		},
	}

	result, err := TranslateRule(source)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	expected := "CRS Rule 123456"

	if result.Rule.Finding.Title != expected {
		t.Errorf(
			"expected title %q, got %q",
			expected,
			result.Rule.Finding.Title,
		)
	}
}
