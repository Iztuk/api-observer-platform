package crs

import (
	"bufio"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParseSecRule(t *testing.T) {
	raw := `SecRule REQUEST_COOKIES|REQUEST_COOKIES_NAMES|ARGS_NAMES|ARGS|XML:/*|XML://@* "@rx (?i)union.*?select.*?from" \
    "id:942270,\
    phase:2,\
    block,\
    capture,\
    t:none,t:urlDecodeUni,\
    msg:'Looking for basic sql injection',\
    logdata:'Matched Data: %{TX.0} found within %{MATCHED_VAR_NAME}: %{MATCHED_VAR}',\
    tag:'application-multi',\
    tag:'attack-sqli',\
    tag:'OWASP_CRS',\
    ver:'OWASP_CRS/4.30.0-dev',\
    severity:'CRITICAL',\
    setvar:'tx.sql_injection_score=+%{tx.critical_anomaly_score}'"`

	got, err := ParseSecRule(raw)
	if err != nil {
		t.Fatalf("ParseSecRule() error = %v", err)
	}

	want := CRSRule{
		Targets: []Target{
			{
				Variable: RequestCookies,
			},
			{
				Variable: RequestCookiesNames,
			},
			{
				Variable: ArgsNames,
			},
			{
				Variable: Args,
			},
			{
				Variable: XML,
				Selector: "/*",
			},
			{
				Variable: XML,
				Selector: "//@*",
			},
		},

		Operator: Operator{
			Type:  OperatorRegex,
			Value: `(?i)union.*?select.*?from`,
		},

		Actions: Actions{
			ID:    "942270",
			Phase: 2,

			Transforms: []Transformation{
				TransformNone,
				TransformURLDecodeUni,
			},

			Message: "Looking for basic sql injection",
			LogData: "Matched Data: %{TX.0} found within " +
				"%{MATCHED_VAR_NAME}: %{MATCHED_VAR}",

			Severity: SeverityCritical,

			Tags: []string{
				"application-multi",
				"attack-sqli",
				"OWASP_CRS",
			},

			Version: "OWASP_CRS/4.30.0-dev",

			Block:   true,
			Capture: true,

			SetVars: []SetVar{
				{
					Collection: "tx",
					Name:       "sql_injection_score",
					Operation:  SetVarIncrement,
					Value:      "%{tx.critical_anomaly_score}",
				},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf(
			"ParseSecRule() mismatch\n\ngot:\n%+v\n\nwant:\n%+v",
			got,
			want,
		)
	}
}

func TestParseSecRule_SQLiCRS(t *testing.T) {
	data, err := os.ReadFile(
		"testdata/REQUEST-942-APPLICATION-ATTACK-SQLI.conf",
	)
	if err != nil {
		t.Fatalf("failed to read CRS test file: %v", err)
	}

	rules := extractSecRules(string(data))

	// This particular SQLi CRS file currently contains 73 SecRule
	// directives, including child rules used by chains.
	if got, want := len(rules), 73; got != want {
		t.Fatalf(
			"unexpected number of SecRule directives: got %d, want %d",
			got,
			want,
		)
	}

	for i, raw := range rules {
		expectedID := extractRuleID(raw)

		name := fmt.Sprintf("rule_%03d", i+1)

		if expectedID != "" {
			name = "rule_" + expectedID
		}

		t.Run(name, func(t *testing.T) {
			got, err := ParseSecRule(raw)
			if err != nil {
				t.Errorf(
					"ParseSecRule() failed: %v\n\nraw rule:\n%s",
					err,
					raw,
				)
				return
			}

			// Every SecRule should have at least one target.
			if len(got.Targets) == 0 {
				t.Errorf(
					"ParseSecRule() returned no targets\n\nraw rule:\n%s",
					raw,
				)
			}

			// Every SecRule should have an operator.
			if got.Operator.Type == "" {
				t.Errorf(
					"ParseSecRule() returned empty operator\n\nraw rule:\n%s",
					raw,
				)
			}

			// Parent rules normally have an ID.
			//
			// Chained child rules may not because they inherit
			// metadata from the parent rule.
			if expectedID != "" && got.Actions.ID != expectedID {
				t.Errorf(
					"wrong rule ID: got %q, want %q",
					got.Actions.ID,
					expectedID,
				)
			}
		})
	}
}

func TestParseRules_Chains(t *testing.T) {
	data, err := os.ReadFile(
		"testdata/REQUEST-942-APPLICATION-ATTACK-SQLI.conf",
	)
	if err != nil {
		t.Fatalf("failed to read CRS test file: %v", err)
	}

	rawRules := extractSecRules(string(data))

	rules, err := ParseRules(rawRules)
	if err != nil {
		t.Fatalf("ParseRules() failed: %v", err)
	}

	for _, rule := range rules {
		if !rule.Actions.Chain {
			continue
		}

		if rule.ChainedRule == nil {
			t.Errorf(
				"rule %q declares chain but has no ChainedRule",
				rule.Actions.ID,
			)
			continue
		}

		if len(rule.ChainedRule.Targets) == 0 {
			t.Errorf(
				"chained rule for %q has no targets",
				rule.Actions.ID,
			)
		}

		if rule.ChainedRule.Operator.Type == "" {
			t.Errorf(
				"chained rule for %q has no operator",
				rule.Actions.ID,
			)
		}
	}
}

func extractSecRules(raw string) []string {
	scanner := bufio.NewScanner(strings.NewReader(raw))

	var rules []string
	var current strings.Builder

	collecting := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if !collecting {
			if !strings.HasPrefix(line, "SecRule ") {
				continue
			}

			collecting = true
		}

		continued := strings.HasSuffix(line, `\`)

		if continued {
			line = strings.TrimSuffix(line, `\`)
			line = strings.TrimSpace(line)
		}

		if current.Len() > 0 {
			current.WriteByte(' ')
		}

		current.WriteString(line)

		if continued {
			continue
		}

		rules = append(rules, current.String())

		current.Reset()
		collecting = false
	}

	if current.Len() > 0 {
		rules = append(rules, current.String())
	}

	return rules
}

var ruleIDRegex = regexp.MustCompile(`\bid:(\d+)`)

func extractRuleID(raw string) string {
	match := ruleIDRegex.FindStringSubmatch(raw)

	if len(match) != 2 {
		return ""
	}

	return match[1]
}
