package query

import (
	"testing"
)

func TestParser(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:  "simple string comparison",
			query: `host = "test-service"`,
		},
		{
			name:  "simple number comparison",
			query: `status >= 400`,
		},
		{
			name:  "and expression",
			query: `status >= 400 AND findings > 0`,
		},
		{
			name:  "or expression",
			query: `status = 404 OR status = 500`,
		},
		{
			name:  "not expression",
			query: `NOT status = 200`,
		},
		{
			name:  "grouped expression",
			query: `(status = 404 OR status = 500) AND findings > 0`,
		},
		{
			name:  "nested grouping",
			query: `host = "test" AND (status = 404 OR (status = 500 AND findings > 0))`,
		},
		{
			name:    "missing value",
			query:   `status =`,
			wantErr: true,
		},
		{
			name:    "missing operator",
			query:   `status 400`,
			wantErr: true,
		},
		{
			name:    "missing identifier",
			query:   `= 400`,
			wantErr: true,
		},
		{
			name:    "missing closing parenthesis",
			query:   `(status = 400`,
			wantErr: true,
		},
		{
			name:    "missing outer closing parenthesis",
			query:   `(host = "string" AND (status = 1 OR status = 2) AND findings > 5`,
			wantErr: true,
		},
		{
			name:    "extra closing parenthesis",
			query:   `status = 400)`,
			wantErr: true,
		},
		{
			name:    "incomplete and",
			query:   `status = 400 AND`,
			wantErr: true,
		},
		{
			name:    "incomplete or",
			query:   `status = 400 OR`,
			wantErr: true,
		},
		{
			name:    "bang without equal",
			query:   `status ! 400`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := Lexer{
				Query:  tt.query,
				Tokens: make([]Token, 0),
			}

			if err := lexer.Process(); err != nil {
				t.Fatalf(
					"unexpected lexer error: %v",
					err,
				)
			}

			parser := Parser{
				Query:  tt.query,
				Tokens: lexer.Tokens,
			}

			_, err := parser.Parse()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected parser error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"unexpected parser error: %v",
					err,
				)
			}
		})
	}
}
