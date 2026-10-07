// Package query handles querying operations for API Observer's dashboard.
package query

import (
	queryv1 "api-observer/proto/query/v1"
)

type LogItem struct {
	Log *queryv1.Log
}

func ParseQuery(
	rawString string,
) (Expression, error) {
	if len(rawString) == 0 {
		return nil, nil
	}

	l := Lexer{
		Query:    rawString,
		Position: 0,
		Tokens:   make([]Token, 0),
	}

	if err := l.Process(); err != nil {
		return nil, err
	}

	p := Parser{
		Query:         rawString,
		Tokens:        l.Tokens,
		TokenPosition: 0,
	}

	expr, err := p.Parse()
	if err != nil {
		return nil, err
	}

	return expr, nil
}
