package query

import (
	"fmt"
	"strings"
)

type QueryError struct {
	Message  string
	Query    string
	Position int
}

func newQueryError(
	query string,
	position int,
	message string,
) error {
	return QueryError{
		Message:  message,
		Query:    query,
		Position: position,
	}
}

func (e QueryError) Error() string {
	position := e.Position

	if position < 0 {
		position = 0
	}

	if position > len(e.Query) {
		position = len(e.Query)
	}

	return fmt.Sprintf(
		"%s at position %d\n\n%s\n%s^",
		e.Message,
		position,
		e.Query,
		strings.Repeat(" ", position),
	)
}
