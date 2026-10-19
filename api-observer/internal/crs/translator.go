package crs

import (
	"api-observer/internal/audit"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type TranslationResults struct {
	Rules map[string]TranslationResult `json:"rules" yaml:"rules"`
}

type TranslationResult struct {
	Rule     *audit.Rule `json:"rule,omitempty" yaml:"rule,omitempty"`
	Warnings []string    `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Error    string      `json:"error,omitempty" yaml:"error,omitempty"`
}

func TranslateRules(source []CRSRule) (TranslationResults, error) {
	results := TranslationResults{
		Rules: make(map[string]TranslationResult),
	}

	for i, src := range source {
		id := fmt.Sprintf("crs-rule-%s", src.Actions.ID)

		if src.Actions.ID == "" {
			id = fmt.Sprintf("crs-rule-import-%d", i+1)
		}

		if err := translateRuleChain(
			src,
			id,
			src.Actions.Phase,
			&results,
		); err != nil {
			return results, err
		}
	}

	return results, nil
}

func translateRuleChain(
	src CRSRule,
	id string,
	inheritedPhase int,
	results *TranslationResults,
) error {
	if _, exists := results.Rules[id]; exists {
		return fmt.Errorf(
			"duplicate translated rule ID %q",
			id,
		)
	}

	results.Rules[id] = TranslationResult{}

	if src.Actions.Phase == 0 {
		src.Actions.Phase = inheritedPhase
	}

	if src.Actions.ID == "" {
		src.Actions.ID = id
	}

	translated, translateErr := TranslateRule(src)

	var childID string

	if src.ChainedRule != nil {
		child := *src.ChainedRule

		childID = id + "-chain"

		if child.Actions.ID != "" {
			childID = fmt.Sprintf(
				"crs-rule-%s",
				child.Actions.ID,
			)
		}

		if err := translateRuleChain(
			child,
			childID,
			src.Actions.Phase,
			results,
		); err != nil {
			return err
		}
	}

	if translateErr != nil {
		results.Rules[id] = TranslationResult{
			Error: translateErr.Error(),
		}

		return nil
	}

	if translated.Rule == nil ||
		len(translated.Rule.Match.Conditions) == 0 {

		results.Rules[id] = TranslationResult{
			Warnings: uniqueWarnings(translated.Warnings),
			Error:    "no supported match conditions",
		}

		return nil
	}

	if childID != "" {
		childResult := results.Rules[childID]

		if childResult.Rule == nil ||
			childResult.Error != "" {

			childError := childResult.Error

			if childError == "" {
				childError = "no usable rule produced"
			}

			results.Rules[id] = TranslationResult{
				Warnings: uniqueWarnings(translated.Warnings),
				Error: fmt.Sprintf(
					"chained rule %s could not be translated: %s",
					childID,
					childError,
				),
			}

			return nil
		}

		translated.Rule.Chain = childID
	}

	results.Rules[id] = translated

	return nil
}

func TranslateRule(src CRSRule) (TranslationResult, error) {
	result := TranslationResult{
		Rule: &audit.Rule{
			Scope: make(audit.RuleScopes, 0),

			Description: fmt.Sprintf(
				"Translated from CRS rule %s",
				src.Actions.ID,
			),

			Match: audit.RuleMatch{
				Mode:       audit.MatchModeAny,
				Conditions: make(audit.MatchConditions, 0),
			},
		},
	}

	scope, err := translatePhaseToScope(src.Actions.Phase)
	if err != nil {
		return TranslationResult{}, err
	}

	result.Rule.Scope = append(
		result.Rule.Scope,
		scope,
	)

	matchOperator, err := translateOperatorToMatchOperator(
		src.Operator,
	)
	if err != nil {
		return TranslationResult{}, err
	}

	conditions, errors := translateTargetMatchConditions(
		src.Targets,
		src.Operator,
		matchOperator,
	)

	result.Rule.Match.Conditions = conditions

	for _, err := range errors {
		result.Warnings = append(
			result.Warnings,
			err.Error(),
		)
	}

	result.Warnings = uniqueWarnings(result.Warnings)

	result.Rule.Finding = translateActionsToFinding(src)

	return result, nil
}

func uniqueWarnings(warnings []string) []string {
	if len(warnings) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(warnings))
	result := make([]string, 0, len(warnings))

	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)

		if warning == "" {
			continue
		}

		if _, exists := seen[warning]; exists {
			continue
		}

		seen[warning] = struct{}{}
		result = append(result, warning)
	}

	return result
}

func (results TranslationResults) RuleSetYAML() ([]byte, error) {
	accepted := make(map[string]*audit.Rule)
	rejected := make(map[string]TranslationResult)

	for id, result := range results.Rules {
		result.Warnings = uniqueWarnings(result.Warnings)

		if result.Error != "" {
			rejected[id] = result
			continue
		}

		if result.Rule == nil {
			result.Error = "translation produced no rule"
			rejected[id] = result
			continue
		}

		// Validate the rule before exporting it.
		if err := validateTranslatedRule(result.Rule); err != nil {
			result.Error = err.Error()
			rejected[id] = result
			continue
		}

		accepted[id] = result.Rule
	}

	for {
		removed := false

		for id, rule := range accepted {
			if rule.Chain == "" {
				continue
			}

			if _, exists := accepted[rule.Chain]; exists {
				continue
			}

			result := results.Rules[id]

			result.Warnings = uniqueWarnings(result.Warnings)

			result.Error = fmt.Sprintf(
				"chained rule %q is not available",
				rule.Chain,
			)

			rejected[id] = result

			delete(accepted, id)

			removed = true
		}

		if !removed {
			break
		}
	}

	acceptedIDs := make([]string, 0, len(accepted))
	rejectedIDs := make([]string, 0, len(rejected))

	for id := range accepted {
		acceptedIDs = append(acceptedIDs, id)
	}

	for id := range rejected {
		rejectedIDs = append(rejectedIDs, id)
	}

	sort.Strings(acceptedIDs)
	sort.Strings(rejectedIDs)

	var output strings.Builder

	if len(acceptedIDs) == 0 {
		output.WriteString("rules: {}\n")
	} else {
		output.WriteString("rules:\n")
	}

	for _, id := range acceptedIDs {
		result := results.Rules[id]

		for _, warning := range uniqueWarnings(result.Warnings) {
			writeRuleComment(
				&output,
				"  # WARNING: ",
				warning,
			)
		}

		key, err := yaml.Marshal(id)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to marshal rule ID %q: %w",
				id,
				err,
			)
		}

		output.WriteString("  ")
		output.WriteString(strings.TrimSpace(string(key)))
		output.WriteString(":\n")

		ruleData, err := yaml.Marshal(accepted[id])
		if err != nil {
			return nil, fmt.Errorf(
				"failed to marshal rule %q: %w",
				id,
				err,
			)
		}

		writeIndentedYAML(
			&output,
			ruleData,
			"    ",
		)
	}

	if _, err := audit.ParseRuleSet(output.String()); err != nil {
		return nil, fmt.Errorf(
			"generated ruleset failed validation: %w",
			err,
		)
	}

	// Write failed rules at the end.
	if len(rejectedIDs) > 0 {
		output.WriteString("\n")

		output.WriteString(
			"# ========================================\n",
		)

		output.WriteString(
			"# Excluded CRS Rules\n",
		)

		output.WriteString(
			"# ========================================\n",
		)

		for _, id := range rejectedIDs {
			result := rejected[id]

			result.Warnings = uniqueWarnings(result.Warnings)

			output.WriteString("\n")

			key, err := yaml.Marshal(id)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to marshal excluded rule ID %q: %w",
					id,
					err,
				)
			}

			output.WriteString("#   ")
			output.WriteString(strings.TrimSpace(string(key)))
			output.WriteString(":\n")

			reportData, err := yaml.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf(
					"failed to marshal excluded rule %q: %w",
					id,
					err,
				)
			}

			writeCommentedYAML(
				&output,
				reportData,
				"    ",
			)
		}
	}

	return []byte(output.String()), nil
}

func validateTranslatedRule(rule *audit.Rule) error {
	if rule == nil {
		return fmt.Errorf("rule is nil")
	}

	if len(rule.Scope) == 0 {
		rule.Scope.DefaultConfiguration()
	}

	if err := rule.Scope.Validate(); err != nil {
		return err
	}

	if err := rule.Match.Validate(); err != nil {
		return err
	}

	for i, condition := range rule.Match.Conditions {
		if condition.Operator != audit.MatchOperatorRegex {
			continue
		}

		if _, err := regexp.Compile(condition.Value); err != nil {
			return fmt.Errorf(
				"condition %d has invalid regex: %w",
				i+1,
				err,
			)
		}
	}

	return nil
}

func writeRuleComment(
	output *strings.Builder,
	prefix string,
	value string,
) {
	for _, line := range strings.Split(value, "\n") {
		output.WriteString(prefix)
		output.WriteString(line)
		output.WriteString("\n")
	}
}

func writeIndentedYAML(
	output *strings.Builder,
	data []byte,
	indent string,
) {
	content := strings.TrimSuffix(string(data), "\n")

	if content == "" {
		return
	}

	for _, line := range strings.Split(content, "\n") {
		output.WriteString(indent)
		output.WriteString(line)
		output.WriteString("\n")
	}
}

func writeCommentedYAML(
	output *strings.Builder,
	data []byte,
	indent string,
) {
	content := strings.TrimSuffix(string(data), "\n")

	if content == "" {
		return
	}

	for _, line := range strings.Split(content, "\n") {
		output.WriteString("# ")
		output.WriteString(indent)
		output.WriteString(line)
		output.WriteString("\n")
	}
}

func translatePhaseToScope(
	phase int,
) (audit.RuleScope, error) {
	switch {
	case phase >= 1 && phase <= 2:
		return audit.RuleScopeRequest, nil

	case phase >= 3 && phase <= 4:
		return audit.RuleScopeResponse, nil

	default:
		return "", fmt.Errorf(
			"unsupported CRS phase: %d",
			phase,
		)
	}
}

func translateOperatorToMatchOperator(
	o Operator,
) (audit.MatchOperator, error) {
	switch o.Type {
	case OperatorRegex:
		return audit.MatchOperatorRegex, nil

	case OperatorDetectSQLi:
		return audit.MatchOperatorDetectSQLi, nil

	case OperatorStringEq:
		return audit.MatchOperatorStringEqual, nil

	case OperatorEqual:
		return audit.MatchOperatorEqual, nil

	case OperatorLessThan:
		return audit.MatchOperatorLessThan, nil

	case OperatorGreaterThan:
		return audit.MatchOperatorGreaterThan, nil

	case OperatorLessThanOrEqual:
		return audit.MatchOperatorLessThanOrEqual, nil

	case OperatorGreaterThanOrEqual:
		return audit.MatchOperatorGreaterThanOrEqual, nil

	default:
		return "", fmt.Errorf(
			"unsupported CRS operator %q",
			o.Type,
		)
	}
}

func translateTargetMatchConditions(
	targets []Target,
	operator Operator,
	matchOperator audit.MatchOperator,
) (audit.MatchConditions, []error) {
	var result audit.MatchConditions
	var errors []error

	for _, target := range targets {
		conditions := make([]audit.MatchCondition, 0)

		switch target.Variable {
		case Args, ArgsNames:
			conditions = append(
				conditions,
				argsToMatchCondition(target)...,
			)

		case RequestBody, ResponseBody:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetBody,
				},
			)

		case RequestBodyLength:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetBodyLength,
				},
			)

		case RequestCookies:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetCookie,
					Key:    target.Selector,
				},
			)

		case RequestCookiesNames:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetCookieName,
				},
			)

		case RequestHeaders, RequestHeadersNames, ResponseHeaders, ResponseHeadersNames:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetHeader,
					Key:    target.Selector,
				},
			)

		case RequestMethod:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetMethod,
				},
			)

		case ResponseContentType:
			conditions = append(
				conditions,
				audit.MatchCondition{
					Target: audit.RuleTargetHeader,
					Key:    "Content-Type",
				},
			)

		default:
			errors = append(
				errors,
				fmt.Errorf(
					"unsupported variable type: %s",
					target.Variable,
				),
			)

			continue
		}

		for _, condition := range conditions {
			result = append(
				result,
				audit.MatchCondition{
					Target:   condition.Target,
					Key:      condition.Key,
					Operator: matchOperator,
					Value:    operator.Value,
					Negated:  operator.Negated,
				},
			)
		}
	}

	return result, errors
}

func argsToMatchCondition(t Target) []audit.MatchCondition {
	return []audit.MatchCondition{
		{
			Target: audit.RuleTargetBody,
		},
		{
			Target: audit.RuleTargetQuery,
			Key:    t.Selector,
		},
	}
}

func translateActionsToFinding(
	source CRSRule,
) audit.RuleFinding {
	title := source.Actions.Message

	if title == "" {
		title = fmt.Sprintf(
			"CRS Rule %s",
			source.Actions.ID,
		)
	}

	return audit.RuleFinding{
		Title:    title,
		Message:  title,
		Severity: string(source.Actions.Severity),
		Tags: append(
			[]string(nil),
			source.Actions.Tags...,
		),
	}
}
