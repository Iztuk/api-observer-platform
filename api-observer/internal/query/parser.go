package query

import (
	"fmt"
	"strconv"
)

type Parser struct {
	Query         string // Used solely for the error handling
	Tokens        []Token
	TokenPosition int
}

func (p *Parser) Parse() (Expression, error) {
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if p.current().Type != TokenEOF {
		token := p.current()

		return nil, newQueryError(
			p.Query,
			token.Position,
			"unexpected token",
		)
	}

	return expr, nil
}

func (p *Parser) parseExpression() (Expression, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (Expression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.current().Type == TokenOr {
		p.advance()

		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}

		left = OrExpression{
			Left:  left,
			Right: right,
		}
	}

	return left, nil
}

func (p *Parser) parseAnd() (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for p.current().Type == TokenAnd {
		p.advance()

		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}

		left = AndExpression{
			Left:  left,
			Right: right,
		}
	}

	return left, nil
}

func (p *Parser) parseUnary() (Expression, error) {
	if p.current().Type == TokenNot {
		p.advance()

		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}

		return NotExpression{
			Expression: expr,
		}, nil
	}

	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expression, error) {
	if p.current().Type == TokenLeftParen {
		opening := p.advance()

		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		if p.current().Type != TokenRightParen {
			return nil, newQueryError(
				p.Query,
				p.current().Position,
				fmt.Sprintf(
					"expected ')' to close '(' at position %d",
					opening.Position,
				),
			)
		}

		p.advance()

		return expr, nil
	}

	return p.parseComparison()
}

func (p *Parser) parseComparison() (Expression, error) {
	fieldToken := p.current()

	if fieldToken.Type != TokenIdentifier {
		return nil, newQueryError(
			p.Query,
			fieldToken.Position,
			"expected identifier",
		)
	}

	p.advance()

	operator, err := p.parseOperator()
	if err != nil {
		return nil, err
	}

	value, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	return ComparisonExpression{
		Field: Field{
			Name:     FieldName(fieldToken.Literal),
			Position: fieldToken.Position,
		},
		Operator: operator,
		Value:    value,
	}, nil
}

func (p *Parser) parseOperator() (Operator, error) {
	token := p.current()

	switch token.Type {
	case TokenEqual:
		p.advance()

		return Operator{
			Name:     "=",
			Type:     OperatorTypeEqual,
			Position: token.Position,
		}, nil

	case TokenGreater:
		p.advance()

		if p.current().Type == TokenEqual {
			p.advance()

			return Operator{
				Name:     ">=",
				Type:     OperatorTypeGreaterEqual,
				Position: token.Position,
			}, nil
		}

		return Operator{
			Name:     ">",
			Type:     OperatorTypeGreater,
			Position: token.Position,
		}, nil

	case TokenLess:
		p.advance()

		if p.current().Type == TokenEqual {
			p.advance()

			return Operator{
				Name:     "<=",
				Type:     OperatorTypeLessEqual,
				Position: token.Position,
			}, nil
		}

		return Operator{
			Name:     "<",
			Type:     OperatorTypeLess,
			Position: token.Position,
		}, nil

	case TokenBang:
		p.advance()

		if p.current().Type == TokenEqual {
			p.advance()

			return Operator{
				Name:     "!=",
				Type:     OperatorTypeNotEqual,
				Position: token.Position,
			}, nil
		} else {
			return Operator{}, newQueryError(
				p.Query,
				token.Position,
				"expected '=' after '!'",
			)
		}

	default:
		return Operator{}, newQueryError(
			p.Query,
			token.Position,
			"expected comparison operator",
		)
	}
}

func (p *Parser) parseValue() (Value, error) {
	token := p.current()

	switch token.Type {
	case TokenString:
		p.advance()

		return Value{
			Value:    token.Literal,
			Type:     ValueTypeString,
			Position: token.Position,
		}, nil

	case TokenNumber:
		p.advance()

		value, err := strconv.Atoi(token.Literal)
		if err != nil {
			return Value{}, newQueryError(
				p.Query,
				token.Position,
				"invalid number",
			)
		}

		return Value{
			Value:    value,
			Type:     ValueTypeNumber,
			Position: token.Position,
		}, nil

	default:
		return Value{}, newQueryError(
			p.Query,
			token.Position,
			"expected string or number",
		)
	}
}

func (p *Parser) current() Token {
	return p.Tokens[p.TokenPosition]
}

func (p *Parser) advance() Token {
	token := p.current()

	if p.TokenPosition < len(p.Tokens)-1 {
		p.TokenPosition++
	}

	return token
}

func (p *Parser) peek(offset int) Token {
	position := p.TokenPosition + offset

	if position >= len(p.Tokens) {
		return p.Tokens[len(p.Tokens)-1]
	}

	return p.Tokens[position]
}
