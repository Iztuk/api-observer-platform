package query

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	queryv1 "api-observer/proto/query/v1"
)

func EvaluateExpression(
	queryString string,
	expr Expression,
	item LogItem,
) (bool, error) {
	if expr == nil {
		return true, nil
	}

	if item.Log == nil {
		return false, nil
	}

	switch expr := expr.(type) {
	case ComparisonExpression:
		return evaluateComparison(
			queryString,
			expr,
			item,
		)

	case AndExpression:
		left, err := EvaluateExpression(
			queryString,
			expr.Left,
			item,
		)
		if err != nil {
			return false, err
		}

		if !left {
			return false, nil
		}

		return EvaluateExpression(
			queryString,
			expr.Right,
			item,
		)

	case OrExpression:
		left, err := EvaluateExpression(
			queryString,
			expr.Left,
			item,
		)
		if err != nil {
			return false, err
		}

		if left {
			return true, nil
		}

		return EvaluateExpression(
			queryString,
			expr.Right,
			item,
		)

	case NotExpression:
		result, err := EvaluateExpression(
			queryString,
			expr.Expression,
			item,
		)
		if err != nil {
			return false, err
		}

		return !result, nil
	}

	return false, fmt.Errorf(
		"unknown expression type %T",
		expr,
	)
}

func evaluateComparison(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	switch expr.Field.Name {
	case "type":
		return evaluateTypeField(
			queryString,
			expr,
			item,
		)

	case "host":
		return evaluateHostField(
			queryString,
			expr,
			item,
		)

	case "method":
		return evaluateMethod(
			queryString,
			expr,
			item,
		)

	case "path":
		return evaluatePath(
			queryString,
			expr,
			item,
		)

	case "status":
		return evaluateStatus(
			queryString,
			expr,
			item,
		)

	case "timestamp":
		return evaluateTimestamp(
			queryString,
			expr,
			item,
		)

	// case "findings":
	// 	return evaluateFindingsCount(
	// 		queryString,
	// 		expr,
	// 		item,
	// 	)

	default:
		return false, newQueryError(
			queryString,
			expr.Field.Position,
			fmt.Sprintf(
				"unsupported field %q",
				expr.Field.Name,
			),
		)
	}
}

func evaluateTypeField(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expected, ok := expr.Value.Value.(string)
	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a string value",
				expr.Field.Name,
			),
		)
	}

	expected = strings.ToLower(expected)

	if !isValidJobType(expected) {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a value of \"request\" or \"response\"",
				expr.Field.Name,
			),
		)
	}

	var actual string

	switch item.Log.Event.(type) {
	case *queryv1.Log_Request:
		actual = "request"

	case *queryv1.Log_Response:
		actual = "response"

	default:
		return false, nil
	}

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return actual == expected, nil

	case OperatorTypeNotEqual:
		return actual != expected, nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are = and !=",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

func isValidJobType(
	value string,
) bool {
	switch strings.ToLower(value) {
	case "request", "response":
		return true

	default:
		return false
	}
}

func evaluateHostField(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expected, ok := expr.Value.Value.(string)
	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a string value",
				expr.Field.Name,
			),
		)
	}

	request := item.Log.GetRequest()
	if request == nil {
		return false, nil
	}

	actual := ""

	for key, values := range request.Headers {
		if !strings.EqualFold(
			key,
			"Host",
		) {
			continue
		}

		if values != nil &&
			len(values.Values) > 0 {
			actual = values.Values[0]
		}

		break
	}

	/*
		If Host isn't available as a header, try the URL.
	*/
	if actual == "" &&
		request.Url != "" {
		u, err := url.Parse(
			request.Url,
		)
		if err == nil {
			actual = u.Host
		}
	}

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return strings.EqualFold(
			actual,
			expected,
		), nil

	case OperatorTypeNotEqual:
		return !strings.EqualFold(
			actual,
			expected,
		), nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are = and !=",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

func evaluateMethod(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expected, ok := expr.Value.Value.(string)
	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a string value",
				expr.Field.Name,
			),
		)
	}

	request := item.Log.GetRequest()
	if request == nil {
		return false, nil
	}

	actual := request.Method

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return strings.EqualFold(
			actual,
			expected,
		), nil

	case OperatorTypeNotEqual:
		return !strings.EqualFold(
			actual,
			expected,
		), nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are = and !=",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

func evaluatePath(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expected, ok := expr.Value.Value.(string)
	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a string value",
				expr.Field.Name,
			),
		)
	}

	request := item.Log.GetRequest()
	if request == nil {
		return false, nil
	}

	actual := request.Url

	if parsedURL, err := url.Parse(
		request.Url,
	); err == nil {
		if parsedURL.Path != "" {
			actual = parsedURL.Path
		}
	}

	matches := pathMatches(
		expected,
		actual,
	)

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return matches, nil

	case OperatorTypeNotEqual:
		return !matches, nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are '=' and '!='",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

func pathMatches(
	pattern string,
	actual string,
) bool {
	patternParts := splitPath(
		pattern,
	)

	actualParts := splitPath(
		actual,
	)

	if len(patternParts) !=
		len(actualParts) {
		return false
	}

	for i := range patternParts {
		patternPart :=
			patternParts[i]

		actualPart :=
			actualParts[i]

		if isPathParameter(
			patternPart,
		) {
			if actualPart == "" {
				return false
			}

			continue
		}

		if patternPart != actualPart {
			return false
		}
	}

	return true
}

func splitPath(
	path string,
) []string {
	path = strings.Trim(
		path,
		"/",
	)

	if path == "" {
		return []string{}
	}

	return strings.Split(
		path,
		"/",
	)
}

func isPathParameter(
	segment string,
) bool {
	return len(segment) >= 3 &&
		segment[0] == '{' &&
		segment[len(segment)-1] == '}'
}

func evaluateStatus(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expected, ok := expr.Value.Value.(int)
	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires a numeric value",
				expr.Field.Name,
			),
		)
	}

	response := item.Log.GetResponse()
	if response == nil {
		return false, nil
	}

	actual := int(
		response.StatusCode,
	)

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return actual == expected, nil

	case OperatorTypeNotEqual:
		return actual != expected, nil

	case OperatorTypeGreater:
		return actual > expected, nil

	case OperatorTypeGreaterEqual:
		return actual >= expected, nil

	case OperatorTypeLess:
		return actual < expected, nil

	case OperatorTypeLessEqual:
		return actual <= expected, nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are '=', '!=', '>', '>=', '<', '<='",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

func evaluateTimestamp(
	queryString string,
	expr ComparisonExpression,
	item LogItem,
) (bool, error) {
	expectedRaw, ok :=
		expr.Value.Value.(string)

	if !ok {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"field %q requires an RFC3339 timestamp string",
				expr.Field.Name,
			),
		)
	}

	expected, err := time.Parse(
		time.RFC3339Nano,
		expectedRaw,
	)
	if err != nil {
		return false, newQueryError(
			queryString,
			expr.Value.Position,
			fmt.Sprintf(
				"invalid timestamp %q for field %q",
				expectedRaw,
				expr.Field.Name,
			),
		)
	}

	var actual time.Time

	switch event := item.Log.Event.(type) {
	case *queryv1.Log_Request:
		if event.Request == nil ||
			event.Request.Metadata == nil ||
			event.Request.Metadata.Timestamp == nil {
			return false, nil
		}

		if err := event.Request.Metadata.Timestamp.CheckValid(); err != nil {
			return false, fmt.Errorf(
				"invalid request timestamp: %w",
				err,
			)
		}

		actual =
			event.Request.Metadata.Timestamp.AsTime()

	case *queryv1.Log_Response:
		if event.Response == nil ||
			event.Response.Metadata == nil ||
			event.Response.Metadata.Timestamp == nil {
			return false, nil
		}

		if err := event.Response.Metadata.Timestamp.CheckValid(); err != nil {
			return false, fmt.Errorf(
				"invalid response timestamp: %w",
				err,
			)
		}

		actual =
			event.Response.Metadata.Timestamp.AsTime()

	default:
		return false, nil
	}

	switch expr.Operator.Type {
	case OperatorTypeEqual:
		return actual.Equal(
			expected,
		), nil

	case OperatorTypeNotEqual:
		return !actual.Equal(
			expected,
		), nil

	case OperatorTypeGreater:
		return actual.After(
			expected,
		), nil

	case OperatorTypeGreaterEqual:
		return actual.After(
			expected,
		) ||
			actual.Equal(
				expected,
			), nil

	case OperatorTypeLess:
		return actual.Before(
			expected,
		), nil

	case OperatorTypeLessEqual:
		return actual.Before(
			expected,
		) ||
			actual.Equal(
				expected,
			), nil

	default:
		return false, newQueryError(
			queryString,
			expr.Operator.Position,
			fmt.Sprintf(
				"operator %q is not supported for field %q; allowed operators are '=', '!=', '>', '>=', '<', '<='",
				expr.Operator.Name,
				expr.Field.Name,
			),
		)
	}
}

// func evaluateFindingsCount(
// 	queryString string,
// 	expr ComparisonExpression,
// 	item LogItem,
// ) (bool, error) {
// 	expected, ok := expr.Value.Value.(int)
// 	if !ok {
// 		return false, newQueryError(
// 			queryString,
// 			expr.Value.Position,
// 			fmt.Sprintf(
// 				"field %q requires a numeric value",
// 				expr.Field.Name,
// 			),
// 		)
// 	}
//
// 	actual := len(
// 		item.Findings,
// 	)
//
// 	switch expr.Operator.Type {
// 	case OperatorTypeEqual:
// 		return actual == expected, nil
//
// 	case OperatorTypeNotEqual:
// 		return actual != expected, nil
//
// 	case OperatorTypeGreater:
// 		return actual > expected, nil
//
// 	case OperatorTypeGreaterEqual:
// 		return actual >= expected, nil
//
// 	case OperatorTypeLess:
// 		return actual < expected, nil
//
// 	case OperatorTypeLessEqual:
// 		return actual <= expected, nil
//
// 	default:
// 		return false, newQueryError(
// 			queryString,
// 			expr.Operator.Position,
// 			fmt.Sprintf(
// 				"operator %q is not supported for field %q; allowed operators are '=', '!=', '>', '>=', '<', '<='",
// 				expr.Operator.Name,
// 				expr.Field.Name,
// 			),
// 		)
// 	}
// }
