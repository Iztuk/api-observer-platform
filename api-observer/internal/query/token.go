package query

type Token struct {
	Type     TokenType
	Literal  string
	Position int
}

type TokenType int

const (
	TokenEOF TokenType = iota

	TokenIdentifier
	TokenString
	TokenNumber

	TokenEqual
	TokenBang
	TokenGreater
	TokenLess

	TokenAnd
	TokenOr
	TokenNot

	TokenLeftParen
	TokenRightParen
)

type Expression any

type ComparisonExpression struct {
	Field    Field
	Operator Operator
	Value    Value
}

type AndExpression struct {
	Left  Expression
	Right Expression
}

type OrExpression struct {
	Left  Expression
	Right Expression
}

type NotExpression struct {
	Expression Expression
}

type Field struct {
	Name     FieldName
	Type     FieldType
	Position int
}

type Operator struct {
	Name     string
	Type     OperatorType
	Position int
}

type Value struct {
	Value    any
	Type     ValueType
	Position int
}

type FieldName string

const (
	FieldNameNode      FieldName = "node"
	FielNameMethod     FieldName = "method"
	FieldNamePath      FieldName = "path"
	FieldNameStatus    FieldName = "status"
	FieldNameTimestamp FieldName = "timestamp"
	FieldNameFindings  FieldName = "findings"
)

type FieldType int

const (
	FieldTypeString FieldType = iota
	FieldTypeNumber
)

type OperatorType int

const (
	OperatorTypeEqual OperatorType = iota
	OperatorTypeNotEqual
	OperatorTypeGreater
	OperatorTypeGreaterEqual
	OperatorTypeLess
	OperatorTypeLessEqual
)

type ValueType int

const (
	ValueTypeString ValueType = iota
	ValueTypeNumber
)
