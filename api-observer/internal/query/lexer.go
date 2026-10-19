package query

import (
	"fmt"
	"strings"
)

type Lexer struct {
	Query    string
	Position int // Refers to the character position in the query string
	Tokens   []Token
}

func (l *Lexer) Process() error {
	for l.Position < len(l.Query) {
		ch := rune(l.Query[l.Position])

		if !isAllowedChar(ch) {
			return newQueryError(
				l.Query,
				l.Position,
				fmt.Sprintf(
					"invalid character %q",
					ch,
				),
			)
		}

		switch {
		case ch == ' ':
			l.Position++

		case ch == '"':
			if err := l.readString(); err != nil {
				return err
			}

		case isOperatorChar(ch):
			if err := l.readOperator(); err != nil {
				return err
			}

		case ch == '(' || ch == ')':
			if err := l.readDelimiter(); err != nil {
				return err
			}

		case isDigit(ch):
			if err := l.readNumber(); err != nil {
				return err
			}

		case isValidIdentifierStart(ch):
			if err := l.readIdentifier(); err != nil {
				return err
			}

		default:
			return newQueryError(
				l.Query,
				l.Position,
				fmt.Sprintf(
					"unexpected character %q",
					ch,
				),
			)
		}
	}

	l.Tokens = append(l.Tokens, Token{
		Type:     TokenEOF,
		Literal:  "",
		Position: len(l.Query),
	})

	return nil
}

func (l *Lexer) readString() error {
	start := l.Position

	l.Position++ // opening quote

	arr := make([]rune, 0)

	for l.Position < len(l.Query) {
		ch := rune(l.Query[l.Position])

		if ch == '"' && !isEscaped(arr) {
			l.Position++

			l.Tokens = append(l.Tokens, Token{
				Type:     TokenString,
				Literal:  string(arr),
				Position: start,
			})

			return nil
		}

		arr = append(arr, ch)
		l.Position++
	}

	return newQueryError(
		l.Query,
		start,
		"unterminated string",
	)
}

func isEscaped(chars []rune) bool {
	if len(chars) == 0 {
		return false
	}

	backslashes := 0

	for i := len(chars) - 1; i >= 0; i-- {
		if chars[i] != '\\' {
			break
		}

		backslashes++
	}

	return backslashes%2 != 0
}

func (l *Lexer) readNumber() error {
	start := l.Position

	arr := make([]rune, 0)

	for l.Position < len(l.Query) {
		ch := rune(l.Query[l.Position])

		if !isAllowedChar(ch) {
			return newQueryError(
				l.Query,
				l.Position,
				fmt.Sprintf(
					"invalid character %q",
					ch,
				),
			)
		}

		if !isDigit(ch) {
			break
		}

		arr = append(arr, ch)
		l.Position++
	}

	l.Tokens = append(l.Tokens, Token{
		Type:     TokenNumber,
		Literal:  string(arr),
		Position: start,
	})

	return nil
}

func (l *Lexer) readOperator() error {
	start := l.Position
	ch := rune(l.Query[l.Position])

	var tokenType TokenType

	switch ch {
	case '=':
		tokenType = TokenEqual

	case '!':
		tokenType = TokenBang

	case '<':
		tokenType = TokenLess

	case '>':
		tokenType = TokenGreater

	default:
		return newQueryError(
			l.Query,
			start,
			fmt.Sprintf(
				"invalid operator %q",
				ch,
			),
		)
	}

	l.Tokens = append(l.Tokens, Token{
		Type:     tokenType,
		Literal:  string(ch),
		Position: start,
	})

	l.Position++

	return nil
}

func (l *Lexer) readDelimiter() error {
	ch := rune(l.Query[l.Position])

	switch ch {
	case '(':
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenLeftParen,
			Literal:  "(",
			Position: l.Position,
		})

		l.Position++

		return nil
	case ')':
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenRightParen,
			Literal:  ")",
			Position: l.Position,
		})

		l.Position++

		return nil

	}

	return newQueryError(
		l.Query,
		l.Position,
		fmt.Sprintf(
			"invalid character %q",
			ch,
		),
	)
}

func (l *Lexer) readIdentifier() error {
	start := l.Position

	arr := make([]rune, 0)

	for l.Position < len(l.Query) {
		ch := rune(l.Query[l.Position])

		if l.Position == start {
			if !isValidIdentifierStart(ch) {
				return newQueryError(
					l.Query,
					l.Position,
					fmt.Sprintf(
						"invalid character %q",
						ch,
					),
				)
			}
		}

		if !isIdentifierChar(ch) {
			break
		}

		arr = append(arr, ch)
		l.Position++
	}

	strArr := strings.ToLower(string(arr))

	switch strArr {
	case "and":
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenAnd,
			Literal:  string(arr),
			Position: start,
		})
	case "or":
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenOr,
			Literal:  string(arr),
			Position: start,
		})
	case "not":
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenNot,
			Literal:  string(arr),
			Position: start,
		})
	default:
		l.Tokens = append(l.Tokens, Token{
			Type:     TokenIdentifier,
			Literal:  string(arr),
			Position: start,
		})
	}

	return nil
}

func isValidIdentifierStart(ch rune) bool {
	return isLetter(ch) || ch == '_'
}

func isIdentifierChar(ch rune) bool {
	return isValidIdentifierStart(ch) || isDigit(ch)
}

func isAllowedChar(ch rune) bool {
	return isLetter(ch) ||
		isDigit(ch) ||
		ch == '_' ||
		ch == '"' ||
		ch == '=' ||
		ch == '<' ||
		ch == '>' ||
		ch == '!' ||
		ch == '(' ||
		ch == ')' ||
		ch == ' '
}

func isOperatorChar(ch rune) bool {
	switch ch {
	case '=', '!', '>', '<':
		return true
	default:
		return false
	}
}

func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
