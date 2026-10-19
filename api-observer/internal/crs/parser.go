package crs

import (
	"fmt"
	"strings"
)

func SplitSecRules(raw string) ([]string, error) {
	var rules []string
	var current strings.Builder

	raw = strings.ReplaceAll(raw, "\r\n", "\n")

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		current.WriteString(line)

		// Count trailing backslashes so an escaped
		// backslash isn't mistaken for a continuation.
		backslashes := 0
		for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
			backslashes++
		}

		if backslashes%2 == 1 {
			current.WriteByte('\n')
			continue
		}

		rules = append(rules, current.String())
		current.Reset()
	}

	if current.Len() > 0 {
		return nil, fmt.Errorf("unfinished rule: trailing line continuation")
	}

	return rules, nil
}

func ParseRules(rawRules []string) ([]CRSRule, error) {
	var rules []CRSRule

	for i := 0; i < len(rawRules); i++ {
		rule, err := ParseSecRule(rawRules[i])
		if err != nil {
			return nil, err
		}

		// Start at the parent rule.
		current := &rule

		// Continue consuming children as long as
		// the current rule declares a chain.
		for current.Actions.Chain {
			i++

			if i >= len(rawRules) {
				return nil, fmt.Errorf(
					"rule %q declares a chain but has no child rule",
					current.Actions.ID,
				)
			}

			child, err := ParseSecRule(rawRules[i])
			if err != nil {
				return nil, err
			}

			current.ChainedRule = &child
			current = current.ChainedRule
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

func ParseSecRule(rawRule string) (CRSRule, error) {
	rawRule = removeLineContinuations(rawRule)

	parts, err := splitSecRule(rawRule)
	if err != nil {
		return CRSRule{}, err
	}

	if len(parts) != 4 {
		return CRSRule{}, fmt.Errorf(
			"expected 4 SecRule components, got %d",
			len(parts),
		)
	}

	var cr CRSRule

	if !parseDirective(parts[0]) {
		return CRSRule{}, fmt.Errorf(
			"configuration directive '%s' not supported",
			parts[0],
		)
	}

	t, err := parseTargets(parts[1])
	if err != nil {
		return CRSRule{}, err
	}
	cr.Targets = t

	o, err := parseOperator(parts[2])
	if err != nil {
		return CRSRule{}, err
	}
	cr.Operator = o

	a, err := parseActions(parts[3])
	if err != nil {
		return CRSRule{}, err
	}
	cr.Actions = a

	return cr, nil
}

func splitSecRule(rule string) ([]string, error) {
	parts := make([]string, 0, 4)

	var sb strings.Builder
	inQuotes := false

	for i := 0; i < len(rule); i++ {
		ch := rule[i]

		switch {
		case ch == '"' && !isEscaped(rule, i):
			inQuotes = !inQuotes
			sb.WriteByte(ch)

		case isWhitespace(ch) && !inQuotes:
			if sb.Len() == 0 {
				continue
			}

			parts = append(parts, sb.String())
			sb.Reset()

		default:
			sb.WriteByte(ch)
		}
	}

	if inQuotes {
		return nil, fmt.Errorf("unterminated quote in SecRule")
	}

	if sb.Len() > 0 {
		parts = append(parts, sb.String())
	}

	return parts, nil
}

func isEscaped(s string, index int) bool {
	backslashes := 0

	for i := index - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashes++
	}

	return backslashes%2 == 1
}

func isWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func parseDirective(directive string) bool {
	return directive == "SecRule"
}

func parseTargets(raw string) ([]Target, error) {
	var targets []Target

	for target := range strings.SplitSeq(raw, "|") {
		target, excluded := strings.CutPrefix(target, "!")

		variableName, selector, _ := strings.Cut(target, ":")

		variable := Variable(variableName)
		if !variable.IsValid() {
			return nil, fmt.Errorf(
				"variable %q not supported",
				variableName,
			)
		}

		targets = append(targets, Target{
			Variable: variable,
			Selector: selector,
			Exclude:  excluded,
		})
	}

	return targets, nil
}

func parseOperator(raw string) (Operator, error) {
	raw = strings.Trim(raw, `"`)

	parts := strings.SplitN(raw, " ", 2)

	operatorName := parts[0]

	operatorName, negated := strings.CutPrefix(operatorName, "!")
	operatorName, _ = strings.CutPrefix(operatorName, "@")

	operatorType := OperatorType(operatorName)

	if !operatorType.IsValid() {
		return Operator{}, fmt.Errorf(
			"operator %q not supported",
			operatorName,
		)
	}

	var value string
	if len(parts) == 2 {
		value = parts[1]
	}

	return Operator{
		Type:    operatorType,
		Value:   value,
		Negated: negated,
	}, nil
}

func parseActions(raw string) (Actions, error) {
	var actions Actions

	raw = strings.Trim(raw, `"`)

	parts, err := splitActions(raw)
	if err != nil {
		return Actions{}, err
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if setter, ok := standaloneActions[part]; ok {
			setter(&actions)
			continue
		}

		name, value, found := strings.Cut(part, ":")
		if !found {
			return Actions{}, fmt.Errorf(
				"unsupported action %q",
				part,
			)
		}

		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)

		setter, ok := valueActions[name]
		if !ok {
			return Actions{}, fmt.Errorf(
				"unsupported action %q",
				name,
			)
		}

		if err := setter(&actions, value); err != nil {
			return Actions{}, err
		}
	}

	return actions, nil
}

func splitActions(raw string) ([]string, error) {
	var parts []string
	var sb strings.Builder

	inSingleQuotes := false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]

		switch {
		case ch == '\'' && !isEscaped(raw, i):
			inSingleQuotes = !inSingleQuotes
			sb.WriteByte(ch)

		case ch == ',' && !inSingleQuotes:
			part := strings.TrimSpace(sb.String())

			if part != "" {
				parts = append(parts, part)
			}

			sb.Reset()

		default:
			sb.WriteByte(ch)
		}
	}

	if inSingleQuotes {
		return nil, fmt.Errorf(
			"unterminated single-quoted action value",
		)
	}

	if sb.Len() > 0 {
		part := strings.TrimSpace(sb.String())

		if part != "" {
			parts = append(parts, part)
		}
	}

	return parts, nil
}

func removeLineContinuations(raw string) string {
	var sb strings.Builder

	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			// Unix newline: \ + \n
			if raw[i+1] == '\n' {
				sb.WriteByte(' ')
				i++
				continue
			}

			// Windows newline: \ + \r\n
			if raw[i+1] == '\r' &&
				i+2 < len(raw) &&
				raw[i+2] == '\n' {

				sb.WriteByte(' ')
				i += 2
				continue
			}
		}

		sb.WriteByte(raw[i])
	}

	return sb.String()
}
