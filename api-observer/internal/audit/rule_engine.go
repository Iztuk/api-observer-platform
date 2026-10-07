package audit

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/corazawaf/libinjection-go"
)

func (rs *RuleSet) Evaluate(j Job) ([]Finding, error) {
	findings := make([]Finding, 0)

	for _, rule := range rs.Rules {
		if rule.Disabled {
			continue
		}

		if rs.evaluateRule(rule, j) {
			findings = append(findings, Finding{
				Title:    rule.Finding.Title,
				Message:  rule.Finding.Message,
				Severity: rule.Finding.Severity,
				Tags:     rule.Finding.Tags,

				Metadata: FindingMetadata{
					Request:  j.Request,
					Response: j.Response,
				},
				ProcessedAt: time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
	}

	return findings, nil
}

func (rs *RuleSet) evaluateRule(rule *Rule, job Job) bool {
	if !rule.Match.EvaluateMatch(job) {
		return false
	}

	if rule.ChainedRule != nil {
		return rs.evaluateRule(rule.ChainedRule, job)
	}

	return true
}

func (r RuleMatch) EvaluateMatch(j Job) bool {
	switch r.Mode {
	case MatchModeAny:
		for _, cond := range r.Conditions {
			if cond.EvaluateCondition(j) {
				return true
			}
		}

		return false

	case MatchModeAll:
		for _, cond := range r.Conditions {
			if !cond.EvaluateCondition(j) {
				return false
			}
		}

		return true

	default:
		return false
	}
}

func (c MatchCondition) EvaluateCondition(j Job) bool {
	if j.Request == nil {
		return false
	}

	switch c.Target {
	case RuleTargetPath:
		return evaluateTargetPath(
			j,
			c.Operator,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetQuery:
		return evaluateTargetQuery(
			j,
			c.Operator,
			c.Key,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetHeader:
		return evaluateTargetHeader(
			j.Request.Header,
			c.Operator,
			c.Key,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetBody:
		return evaluateTargetBody(
			j.Request.Body,
			c.Operator,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetBodyLength:
		return evaluateTargetBodyLength(
			j.Request.ContentLength,
			c.Operator,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetMethod:
		return evaluateTargetMethod(
			j.Request.Method,
			c.Operator,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetCookie:
		return evaluateTargetCookie(
			getCookies(j.Request.Header),
			c.Operator,
			c.Key,
			c.Value,
			c.Negated,
			c.Regex,
		)

	case RuleTargetCookieName:
		return evaluateTargetCookieName(
			getCookies(j.Request.Header),
			c.Operator,
			c.Value,
			c.Negated,
			c.Regex,
		)

	default:
		return false
	}
}

func evaluateTargetPath(
	j Job,
	op MatchOperator,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	path := j.Request.URL.Path

	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(path)
		}

	case MatchOperatorStringEqual:
		found = path == val

	case MatchOperatorDetectSQLi:
		found = isSQLi(path)

	default:
		return false
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetQuery(
	j Job,
	op MatchOperator,
	key,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	query := j.Request.URL.Query()

	values := make([]string, 0)

	if key != "" {
		vals, ok := query[key]
		if !ok {
			return neg
		}

		values = append(values, vals...)
	} else {
		for _, vals := range query {
			values = append(values, vals...)
		}
	}

	str := strings.Join(values, ", ")

	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(str)
		}

	case MatchOperatorStringEqual:
		for _, v := range values {
			if v == val {
				found = true
				break
			}
		}

	case MatchOperatorEqual,
		MatchOperatorNotEqual,
		MatchOperatorLessThan,
		MatchOperatorGreaterThan,
		MatchOperatorLessThanOrEqual,
		MatchOperatorGreaterThanOrEqual:

		for _, v := range values {
			if compareNumeric(v, val, op) {
				found = true
				break
			}
		}

	case MatchOperatorDetectSQLi:
		found = isSQLi(str)
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetHeader(
	header http.Header,
	op MatchOperator,
	key,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	values := make([]string, 0)

	if key != "" {
		values = header.Values(key)

		if len(values) == 0 {
			return neg
		}
	} else {
		for _, vals := range header {
			values = append(values, vals...)
		}
	}

	str := strings.Join(values, ", ")

	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(str)
		}

	case MatchOperatorStringEqual:
		for _, v := range values {
			if v == val {
				found = true
				break
			}
		}

	case MatchOperatorEqual,
		MatchOperatorNotEqual,
		MatchOperatorLessThan,
		MatchOperatorGreaterThan,
		MatchOperatorLessThanOrEqual,
		MatchOperatorGreaterThanOrEqual:

		for _, v := range values {
			if compareNumeric(v, val, op) {
				found = true
				break
			}
		}

	case MatchOperatorDetectSQLi:
		found = isSQLi(str)
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetBody(
	body string,
	op MatchOperator,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(body)
		}

	case MatchOperatorStringEqual:
		found = body == val

	case MatchOperatorDetectSQLi:
		found = isSQLi(body)

	default:
		return false
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetBodyLength(
	contentLength int64,
	op MatchOperator,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	cl := strconv.Itoa(int(contentLength))

	found := false

	switch op {
	case MatchOperatorEqual,
		MatchOperatorNotEqual,
		MatchOperatorLessThan,
		MatchOperatorGreaterThan,
		MatchOperatorLessThanOrEqual,
		MatchOperatorGreaterThanOrEqual:

		found = compareNumeric(cl, val, op)

	default:
		return false
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetMethod(
	method string,
	op MatchOperator,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(method)
		}

	case MatchOperatorStringEqual:
		found = method == val

	default:
		return false
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetCookie(
	cookies []*http.Cookie,
	op MatchOperator,
	key,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	values := make([]string, 0)

	if key != "" {
		for _, cookie := range cookies {
			if cookie.Name == key {
				values = append(values, cookie.Value)
			}
		}

		if len(values) == 0 {
			return neg
		}
	} else {
		for _, cookie := range cookies {
			values = append(values, cookie.Value)
		}
	}

	str := strings.Join(values, ", ")

	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(str)
		}

	case MatchOperatorStringEqual:
		for _, v := range values {
			if v == val {
				found = true
				break
			}
		}

	case MatchOperatorEqual,
		MatchOperatorNotEqual,
		MatchOperatorLessThan,
		MatchOperatorGreaterThan,
		MatchOperatorLessThanOrEqual,
		MatchOperatorGreaterThanOrEqual:

		for _, v := range values {
			if compareNumeric(v, val, op) {
				found = true
				break
			}
		}

	case MatchOperatorDetectSQLi:
		found = isSQLi(str)
	}

	if neg {
		return !found
	}

	return found
}

func evaluateTargetCookieName(
	cookies []*http.Cookie,
	op MatchOperator,
	val string,
	neg bool,
	r *regexp.Regexp,
) bool {
	values := make([]string, 0, len(cookies))

	for _, cookie := range cookies {
		values = append(values, cookie.Name)
	}

	str := strings.Join(values, ", ")

	found := false

	switch op {
	case MatchOperatorRegex:
		if r != nil {
			found = r.MatchString(str)
		}

	case MatchOperatorStringEqual:
		for _, v := range values {
			if v == val {
				found = true
				break
			}
		}

	case MatchOperatorDetectSQLi:
		found = isSQLi(str)

	default:
		return false
	}

	if neg {
		return !found
	}

	return found
}

func getCookies(header http.Header) []*http.Cookie {
	req := &http.Request{
		Header: header,
	}

	return req.Cookies()
}

func compareNumeric(left, right string, op MatchOperator) bool {
	l, err := strconv.ParseFloat(left, 64)
	if err != nil {
		return false
	}

	r, err := strconv.ParseFloat(right, 64)
	if err != nil {
		return false
	}

	switch op {
	case MatchOperatorEqual:
		return l == r

	case MatchOperatorNotEqual:
		return l != r

	case MatchOperatorLessThan:
		return l < r

	case MatchOperatorGreaterThan:
		return l > r

	case MatchOperatorLessThanOrEqual:
		return l <= r

	case MatchOperatorGreaterThanOrEqual:
		return l >= r

	default:
		return false
	}
}

func isSQLi(v string) bool {
	result, _ := libinjection.IsSQLi(v)

	return result
}
