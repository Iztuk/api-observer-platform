package audit

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestRuleSetEvaluate(t *testing.T) {
	reqURL, err := url.Parse(
		"http://localhost:8080/admin?id=5&user=admin",
	)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	job := Job{
		Type: JobTypeRequest,

		Request: &RequestJob{
			Method: http.MethodGet,
			URL:    reqURL,

			Header: http.Header{
				"User-Agent": []string{
					"curl/8.20.0",
				},
				"Cookie": []string{
					"session_id=abc123; role=admin",
				},
			},

			Body:          "",
			ContentLength: 0,

			Metadata: Metadata{
				RequestID: "request-123",
				Source:    "test-app",
			},
		},
	}

	rs := &RuleSet{
		Rules: map[string]*Rule{
			"admin-path": {
				Match: RuleMatch{
					Mode: MatchModeAll,
					Conditions: MatchConditions{
						{
							Target:   RuleTargetPath,
							Operator: MatchOperatorStringEqual,
							Value:    "/admin",
						},
					},
				},

				Finding: RuleFinding{
					Title:    "Admin Path Access",
					Message:  "Request accessed the admin path",
					Severity: "warning",
					Tags: []string{
						"admin",
						"path",
					},
				},
			},

			"admin-cookie": {
				Match: RuleMatch{
					Mode: MatchModeAll,
					Conditions: MatchConditions{
						{
							Target:   RuleTargetCookie,
							Key:      "role",
							Operator: MatchOperatorStringEqual,
							Value:    "admin",
						},
					},
				},

				Finding: RuleFinding{
					Title:    "Admin Cookie",
					Message:  "Admin role cookie detected",
					Severity: "notice",
					Tags: []string{
						"cookie",
						"admin",
					},
				},
			},

			"admin-query": {
				Match: RuleMatch{
					Mode: MatchModeAll,
					Conditions: MatchConditions{
						{
							Target:   RuleTargetQuery,
							Key:      "user",
							Operator: MatchOperatorStringEqual,
							Value:    "admin",
						},
					},
				},

				Finding: RuleFinding{
					Title:    "Admin Query Parameter",
					Message:  "Admin user query parameter detected",
					Severity: "info",
					Tags: []string{
						"query",
					},
				},
			},

			"should-not-match": {
				Match: RuleMatch{
					Mode: MatchModeAll,
					Conditions: MatchConditions{
						{
							Target:   RuleTargetMethod,
							Operator: MatchOperatorStringEqual,
							Value:    http.MethodPost,
						},
					},
				},

				Finding: RuleFinding{
					Title:   "POST Request",
					Message: "This should not be returned",
				},
			},
		},
	}

	findings, err := rs.Evaluate(job)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}

	fmt.Printf("findings returned: %d\n", len(findings))

	for _, finding := range findings {
		fmt.Printf(
			"finding: title=%q message=%q severity=%q request_id=%q source=%q\n",
			finding.Title,
			finding.Message,
			finding.Severity,
			finding.Metadata.RequestID,
			finding.Metadata.Source,
		)
	}

	if len(findings) != 3 {
		t.Fatalf(
			"expected 3 findings, got %d",
			len(findings),
		)
	}
}
