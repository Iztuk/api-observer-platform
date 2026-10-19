package query

import "testing"

func TestLexer(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedTokens []Token
		wantErr        bool
	}{
		{
			name:  "simple string comparison",
			query: `host = "test-service"`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "host",
					Position: 0,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 5,
				},
				{
					Type:     TokenString,
					Literal:  "test-service",
					Position: 7,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 21,
				},
			},
		},
		{
			name:  "number comparison",
			query: `status >= 400`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 0,
				},
				{
					Type:     TokenGreater,
					Literal:  ">",
					Position: 7,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 8,
				},
				{
					Type:     TokenNumber,
					Literal:  "400",
					Position: 10,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 13,
				},
			},
		},
		{
			name:  "not equal comparison",
			query: `status != 200`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 0,
				},
				{
					Type:     TokenBang,
					Literal:  "!",
					Position: 7,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 8,
				},
				{
					Type:     TokenNumber,
					Literal:  "200",
					Position: 10,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 13,
				},
			},
		},
		{
			name:  "and expression",
			query: `status >= 400 AND findings > 0`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 0,
				},
				{
					Type:     TokenGreater,
					Literal:  ">",
					Position: 7,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 8,
				},
				{
					Type:     TokenNumber,
					Literal:  "400",
					Position: 10,
				},
				{
					Type:     TokenAnd,
					Literal:  "AND",
					Position: 14,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "findings",
					Position: 18,
				},
				{
					Type:     TokenGreater,
					Literal:  ">",
					Position: 27,
				},
				{
					Type:     TokenNumber,
					Literal:  "0",
					Position: 29,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 30,
				},
			},
		},
		{
			name:  "or expression",
			query: `status = 404 OR status = 500`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 0,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 7,
				},
				{
					Type:     TokenNumber,
					Literal:  "404",
					Position: 9,
				},
				{
					Type:     TokenOr,
					Literal:  "OR",
					Position: 13,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 16,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 23,
				},
				{
					Type:     TokenNumber,
					Literal:  "500",
					Position: 25,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 28,
				},
			},
		},
		{
			name:  "not expression",
			query: `NOT status = 200`,
			expectedTokens: []Token{
				{
					Type:     TokenNot,
					Literal:  "NOT",
					Position: 0,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 4,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 11,
				},
				{
					Type:     TokenNumber,
					Literal:  "200",
					Position: 13,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 16,
				},
			},
		},
		{
			name:  "grouped expression",
			query: `(status = 400)`,
			expectedTokens: []Token{
				{
					Type:     TokenLeftParen,
					Literal:  "(",
					Position: 0,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 1,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 8,
				},
				{
					Type:     TokenNumber,
					Literal:  "400",
					Position: 10,
				},
				{
					Type:     TokenRightParen,
					Literal:  ")",
					Position: 13,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 14,
				},
			},
		},
		{
			name:  "nested grouping",
			query: `(status = 1 OR status = 2)`,
			expectedTokens: []Token{
				{
					Type:     TokenLeftParen,
					Literal:  "(",
					Position: 0,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 1,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 8,
				},
				{
					Type:     TokenNumber,
					Literal:  "1",
					Position: 10,
				},
				{
					Type:     TokenOr,
					Literal:  "OR",
					Position: 12,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 15,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 22,
				},
				{
					Type:     TokenNumber,
					Literal:  "2",
					Position: 24,
				},
				{
					Type:     TokenRightParen,
					Literal:  ")",
					Position: 25,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 26,
				},
			},
		},
		{
			name:  "identifier with underscore and number",
			query: `request_id2 = "test"`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "request_id2",
					Position: 0,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 12,
				},
				{
					Type:     TokenString,
					Literal:  "test",
					Position: 14,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 20,
				},
			},
		},
		{
			name:  "keywords are case insensitive",
			query: `status = 1 and status = 2 Or NOT findings = 0`,
			expectedTokens: []Token{
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 0,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 7,
				},
				{
					Type:     TokenNumber,
					Literal:  "1",
					Position: 9,
				},
				{
					Type:     TokenAnd,
					Literal:  "and",
					Position: 11,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "status",
					Position: 15,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 22,
				},
				{
					Type:     TokenNumber,
					Literal:  "2",
					Position: 24,
				},
				{
					Type:     TokenOr,
					Literal:  "Or",
					Position: 26,
				},
				{
					Type:     TokenNot,
					Literal:  "NOT",
					Position: 29,
				},
				{
					Type:     TokenIdentifier,
					Literal:  "findings",
					Position: 33,
				},
				{
					Type:     TokenEqual,
					Literal:  "=",
					Position: 42,
				},
				{
					Type:     TokenNumber,
					Literal:  "0",
					Position: 44,
				},
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 45,
				},
			},
		},
		{
			name:  "empty query produces eof",
			query: "",
			expectedTokens: []Token{
				{
					Type:     TokenEOF,
					Literal:  "",
					Position: 0,
				},
			},
		},
		{
			name:    "invalid character",
			query:   `status @ 400`,
			wantErr: true,
		},
		{
			name:    "unterminated string",
			query:   `host = "test-service`,
			wantErr: true,
		},
		{
			name:    "decimal number is invalid",
			query:   `status = 400.5`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := Lexer{
				Query:  tt.query,
				Tokens: make([]Token, 0),
			}

			err := lexer.Process()

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected lexer error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"unexpected lexer error: %v",
					err,
				)
			}

			if len(lexer.Tokens) != len(tt.expectedTokens) {
				t.Fatalf(
					"expected %d tokens, got %d\nexpected: %#v\ngot: %#v",
					len(tt.expectedTokens),
					len(lexer.Tokens),
					tt.expectedTokens,
					lexer.Tokens,
				)
			}

			for i, expected := range tt.expectedTokens {
				actual := lexer.Tokens[i]

				if actual.Type != expected.Type {
					t.Errorf(
						"token %d: expected type %v, got %v",
						i,
						expected.Type,
						actual.Type,
					)
				}

				if actual.Literal != expected.Literal {
					t.Errorf(
						"token %d: expected literal %q, got %q",
						i,
						expected.Literal,
						actual.Literal,
					)
				}

				if actual.Position != expected.Position {
					t.Errorf(
						"token %d: expected position %d, got %d",
						i,
						expected.Position,
						actual.Position,
					)
				}
			}

			if lexer.Position != len(tt.query) {
				t.Errorf(
					"expected lexer position %d, got %d",
					len(tt.query),
					lexer.Position,
				)
			}
		})
	}
}
