package audit

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type RuleSet struct {
	Rules map[string]*Rule `json:"rules" yaml:"rules"`
}

type Rule struct {
	Disabled    bool       `json:"disabled" yaml:"disabled"`
	Scope       RuleScopes `json:"scope" yaml:"scope"`
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`

	Match   RuleMatch   `json:"match" yaml:"match"`
	Finding RuleFinding `json:"finding" yaml:"finding"`

	Chain       string `json:"chain,omitempty" yaml:"chain,omitempty"` // HostRule name will be the reference to the chained HostRule
	ChainedRule *Rule  `json:"-" yaml:"-"`
}

type RuleScopes []RuleScope

type RuleScope string

const (
	RuleScopeRequest  RuleScope = "request"
	RuleScopeResponse RuleScope = "response"
)

type RuleMatch struct {
	Mode       MatchMode       `json:"mode,omitempty" yaml:"mode,omitempty"`
	Conditions MatchConditions `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

type MatchMode string

const (
	MatchModeAll MatchMode = "all"
	MatchModeAny MatchMode = "any"
)

type MatchConditions []MatchCondition

type MatchCondition struct {
	Target RuleTarget `json:"target" yaml:"target"`
	Key    string     `json:"key,omitempty" yaml:"key,omitempty"` // Only applies to key value collections

	Operator MatchOperator `json:"operator" yaml:"operator"`
	Value    string        `json:"value,omitempty" yaml:"value,omitempty"`
	Negated  bool          `json:"negated,omitempty" yaml:"negated,omitempty"`

	Regex *regexp.Regexp `json:"-" yaml:"-"`
}

type RuleTarget string

const (
	RuleTargetPath       RuleTarget = "path"
	RuleTargetQuery      RuleTarget = "query"
	RuleTargetHeader     RuleTarget = "header"
	RuleTargetBody       RuleTarget = "body"
	RuleTargetBodyLength RuleTarget = "body_length"
	RuleTargetMethod     RuleTarget = "method"
	RuleTargetCookie     RuleTarget = "cookie"
	RuleTargetCookieName RuleTarget = "cookie_name"
)

type MatchOperator string

const (
	MatchOperatorRegex       MatchOperator = "regex"
	MatchOperatorStringEqual MatchOperator = "string_equal"
	MatchOperatorDetectSQLi  MatchOperator = "detect_sqli"

	MatchOperatorEqual    MatchOperator = "equal"
	MatchOperatorNotEqual MatchOperator = "not_equal"

	MatchOperatorLessThan    MatchOperator = "less_than"
	MatchOperatorGreaterThan MatchOperator = "greater_than"

	MatchOperatorLessThanOrEqual    MatchOperator = "less_than_or_equal"
	MatchOperatorGreaterThanOrEqual MatchOperator = "greater_than_or_equal"
)

type RuleFinding struct {
	Title    string   `json:"title" yaml:"title"`
	Message  string   `json:"message" yaml:"message"`
	Severity string   `json:"severity,omitempty" yaml:"severity,omitempty"`
	Tags     []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type RuleValidation interface {
	Validate() error
	DefaultConfiguration()
}

func (s RuleScopes) Validate() error {
	for _, scope := range s {
		switch scope {
		case RuleScopeRequest, RuleScopeResponse:
			continue
		default:
			return fmt.Errorf("invalid rule scope %q", scope)
		}
	}

	return nil
}

func (s *RuleScopes) DefaultConfiguration() {
	if len(*s) != 0 {
		return
	}

	*s = RuleScopes{
		RuleScopeRequest,
		RuleScopeResponse,
	}
}

func (m *RuleMatch) Validate() error {
	if m.Mode == "" {
		m.Mode.DefaultConfiguration()
	} else {
		switch m.Mode {
		case MatchModeAll, MatchModeAny:
		default:
			return fmt.Errorf("invalid match mode %q", m.Mode)
		}
	}

	if len(m.Conditions) == 0 {
		return fmt.Errorf("rule matching requires at least 1 condition")
	}

	if err := m.Conditions.Validate(); err != nil {
		return err
	}

	return nil
}

func (m *RuleMatch) DefaultConfiguration() {}

func (m MatchMode) Validate() error {
	return nil
}

func (m *MatchMode) DefaultConfiguration() {
	if *m != "" {
		return
	}

	*m = MatchModeAll
}

func (c MatchConditions) Validate() error {
	for _, cond := range c {
		if err := cond.Target.Validate(); err != nil {
			return err
		}

		if err := cond.Operator.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *MatchConditions) DefaultConfiguration() {}

func (t RuleTarget) Validate() error {
	switch t {
	case RuleTargetPath, RuleTargetQuery, RuleTargetHeader, RuleTargetBody, RuleTargetBodyLength, RuleTargetMethod, RuleTargetCookie, RuleTargetCookieName:
		return nil
	default:
		return fmt.Errorf("invalid rule target %q", t)
	}
}

func (t *RuleTarget) DefaultConfiguration() {}

func (o MatchOperator) Validate() error {
	switch o {
	case MatchOperatorRegex, MatchOperatorStringEqual, MatchOperatorDetectSQLi, MatchOperatorEqual, MatchOperatorNotEqual, MatchOperatorGreaterThan, MatchOperatorGreaterThanOrEqual, MatchOperatorLessThan, MatchOperatorLessThanOrEqual:
		return nil
	default:
		return fmt.Errorf("invalid match operator %q", o)
	}
}

func (o *MatchOperator) DefaultConfiguration() {}

func ParseRuleSet(content string) (*RuleSet, error) {
	if strings.TrimSpace(content) == "" {
		return NewRuleSet(), nil
	}

	var ruleset RuleSet

	if err := yaml.Unmarshal([]byte(content), &ruleset); err != nil {
		return nil, fmt.Errorf("failed to parse ruleset: %w", err)
	}

	if ruleset.Rules == nil {
		return NewRuleSet(), nil
	}

	for _, rule := range ruleset.Rules {
		if len(rule.Scope) == 0 {
			rule.Scope.DefaultConfiguration()
		} else {
			err := rule.Scope.Validate()
			if err != nil {
				return nil, err
			}
		}

		if err := rule.Match.Validate(); err != nil {
			return nil, err
		}
	}

	if err := ruleset.chainRules(); err != nil {
		return nil, err
	}

	if err := ruleset.validateChains(); err != nil {
		return nil, err
	}

	if err := ruleset.CompilePatterns(); err != nil {
		return nil, err
	}

	return &ruleset, nil
}

func NewRuleSet() *RuleSet {
	return &RuleSet{
		Rules: make(map[string]*Rule),
	}
}

func (rs *RuleSet) CompilePatterns() error {
	for ruleID, rule := range rs.Rules {
		for i := range rule.Match.Conditions {
			pattern := &rule.Match.Conditions[i]

			if pattern.Operator != MatchOperatorRegex {
				continue
			}

			re, err := regexp.Compile(pattern.Value)
			if err != nil {
				return fmt.Errorf(
					"host rule %q has invalid regex pattern %q: %w",
					ruleID,
					pattern.Value,
					err,
				)
			}

			pattern.Regex = re
		}
	}

	return nil
}

func (rs *RuleSet) chainRules() error {
	for id, rule := range rs.Rules {
		if rule.Chain == "" {
			continue
		}

		chainedRule, ok := rs.Rules[rule.Chain]
		if !ok {
			return fmt.Errorf("failed to chain rule '%s' to rule '%s'", id, rule.Chain)
		}

		rule.ChainedRule = chainedRule

	}

	return nil
}

func (rs *RuleSet) validateChains() error {
	for id, rule := range rs.Rules {
		visited := make(map[*Rule]bool)

		current := rule

		for current != nil {
			if visited[current] {
				return fmt.Errorf(
					"cycle detected in rule chain starting at %q",
					id,
				)
			}

			visited[current] = true
			current = current.ChainedRule
		}
	}

	return nil
}
