// Package crs supports the OWASP Core Rule Set (CRS).
package crs

import (
	"fmt"
	"strconv"
	"strings"
)

type CRSRule struct {
	Targets     []Target
	Operator    Operator
	Actions     Actions
	ChainedRule *CRSRule
}

type Variable string

const (
	Args      Variable = "ARGS" // Maps to body and query
	ArgsNames Variable = "ARGS_NAMES"

	RequestBody       Variable = "REQUEST_BODY"
	RequestBodyLength Variable = "REQUEST_BODY_LENGTH"

	RequestCookies      Variable = "REQUEST_COOKIES"
	RequestCookiesNames Variable = "REQUEST_COOKIES_NAMES"

	RequestHeaders      Variable = "REQUEST_HEADERS"
	RequestHeadersNames Variable = "REQUEST_HEADERS_NAMES"

	RequestMethod   Variable = "REQUEST_METHOD"
	RequestFilename Variable = "REQUEST_FILENAME"
	RequestBasename Variable = "REQUEST_BASENAME"

	ResponseBody          Variable = "RESPONSE_BODY"
	ResponseContentLength Variable = "RESPONSE_CONTENT_LENGTH"
	ResponseContentType   Variable = "RESPONSE_CONTENT_TYPE"

	ResponseHeaders      Variable = "RESPONSE_HEADERS"
	ResponseHeadersNames Variable = "RESPONSE_HEADERS_NAMES"

	ResponseProtocol Variable = "RESPONSE_PROTOCOL"
	ResponseStatus   Variable = "RESPONSE_STATUS"

	XML Variable = "XML"

	TX               Variable = "TX"
	MatchedVar       Variable = "MATCHED_VAR"
	MatchedVarName   Variable = "MATCHED_VAR_NAME"
	MatchedVars      Variable = "MATCHED_VARS"
	MatchedVarsNames Variable = "MATCHED_VARS_NAMES"
)

func (v Variable) IsValid() bool {
	switch v {
	case Args, ArgsNames,
		RequestBody, RequestBodyLength, RequestCookies, RequestCookiesNames, RequestHeaders, RequestHeadersNames, RequestMethod, RequestFilename, RequestBasename,
		ResponseBody, ResponseContentLength, ResponseContentType, ResponseHeaders, ResponseHeadersNames,
		XML, TX, MatchedVar, MatchedVarName, MatchedVars, MatchedVarsNames:
		return true
	default:
		return false
	}
}

type Target struct {
	Variable Variable
	Selector string // Field to check
	Exclude  bool
}

type OperatorType string

const (
	OperatorRegex      OperatorType = "rx"
	OperatorDetectSQLi OperatorType = "detectSQLi"
	OperatorStringEq   OperatorType = "streq"

	OperatorEqual              OperatorType = "eq"
	OperatorGreaterThan        OperatorType = "gt"
	OperatorGreaterThanOrEqual OperatorType = "ge"
	OperatorLessThan           OperatorType = "lt"
	OperatorLessThanOrEqual    OperatorType = "le"
)

func (ot OperatorType) IsValid() bool {
	switch ot {
	case OperatorRegex, OperatorDetectSQLi, OperatorStringEq, OperatorLessThan:
		return true
	default:
		return false
	}
}

type Operator struct {
	Type    OperatorType
	Value   string
	Negated bool
}

type Actions struct {
	ID         string
	Phase      int
	Transforms []Transformation

	Message  string
	LogData  string
	Severity Severity
	Tags     []string
	Version  string

	// Standalone actions
	Block      bool
	Pass       bool
	Log        bool
	NoLog      bool
	Capture    bool
	MultiMatch bool

	SkipAfter string
	SetVars   []SetVar

	// Rule chaining
	Chain bool
}

var standaloneActions = map[string]func(*Actions){
	"block": func(a *Actions) {
		a.Block = true
	},
	"pass": func(a *Actions) {
		a.Pass = true
	},
	"log": func(a *Actions) {
		a.Log = true
	},
	"nolog": func(a *Actions) {
		a.NoLog = true
	},
	"capture": func(a *Actions) {
		a.Capture = true
	},
	"multiMatch": func(a *Actions) {
		a.MultiMatch = true
	},
	"chain": func(a *Actions) {
		a.Chain = true
	},
}

var valueActions = map[string]func(*Actions, string) error{
	"id": func(a *Actions, value string) error {
		a.ID = value
		return nil
	},

	"phase": func(a *Actions, value string) error {
		phase, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid phase %q: %w", value, err)
		}

		a.Phase = phase
		return nil
	},

	"msg": func(a *Actions, value string) error {
		a.Message = strings.Trim(value, "'")
		return nil
	},

	"logdata": func(a *Actions, value string) error {
		a.LogData = strings.Trim(value, "'")
		return nil
	},

	"severity": func(a *Actions, value string) error {
		value = strings.Trim(value, "'")

		severity := Severity(value)
		a.Severity = severity
		return nil
	},

	"tag": func(a *Actions, value string) error {
		a.Tags = append(a.Tags, strings.Trim(value, "'"))
		return nil
	},

	"ver": func(a *Actions, value string) error {
		a.Version = strings.Trim(value, "'")
		return nil
	},

	// Transforms
	"t": func(a *Actions, s string) error {
		if !Transformation(s).IsValidTransformation() {
			return fmt.Errorf("unsupported transform: %s", s)
		}

		a.Transforms = append(a.Transforms, Transformation(s))
		return nil
	},

	// Set Variable
	"setvar": func(a *Actions, s string) error {
		setVar, err := parseSetVar(s)
		if err != nil {
			return err
		}

		a.SetVars = append(a.SetVars, setVar)

		return nil
	},
	"skipAfter": func(a *Actions, s string) error {
		a.SkipAfter = strings.TrimSpace(s)
		return nil
	},
}

type Transformation string

const (
	TransformNone               Transformation = "none"
	TransformUTF8ToUnicode      Transformation = "utf8toUnicode"
	TransformURLDecodeUni       Transformation = "urlDecodeUni"
	TransformRemoveNulls        Transformation = "removeNulls"
	TransformReplaceNulls       Transformation = "replaceNulls"
	TransformReplaceComments    Transformation = "replaceComments"
	TransformRemoveComments     Transformation = "removeComments"
	TransformRemoveCommentsChar Transformation = "removeCommentsChar"
	TransformRemoveWhitespace   Transformation = "removeWhitespace"
)

func (t Transformation) IsValidTransformation() bool {
	switch t {
	case TransformNone, TransformUTF8ToUnicode, TransformURLDecodeUni, TransformRemoveNulls, TransformReplaceNulls, TransformReplaceComments, TransformRemoveComments, TransformRemoveCommentsChar, TransformRemoveWhitespace:
		return true
	default:
		return false
	}
}

type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityError    Severity = "ERROR"
	SeverityWarning  Severity = "WARNING"
	SeverityNotice   Severity = "NOTICE"
)

type SetVarOperation string

const (
	SetVarAssign    SetVarOperation = "="
	SetVarIncrement SetVarOperation = "+="
	SetVarDecrement SetVarOperation = "-="
)

type SetVar struct {
	Collection string
	Name       string
	Operation  SetVarOperation
	Value      string
}

func parseSetVar(raw string) (SetVar, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "'")

	collectionAndRest, value, found := strings.Cut(raw, "=")
	if !found {
		return SetVar{}, fmt.Errorf("invalid setvar %q: missing '='", raw)
	}

	collection, name, found := strings.Cut(collectionAndRest, ".")
	if !found {
		return SetVar{}, fmt.Errorf("invalid setvar %q: missing collection", raw)
	}

	operation := SetVarAssign

	switch {
	case strings.HasPrefix(value, "+"):
		operation = SetVarIncrement
		value = strings.TrimPrefix(value, "+")

	case strings.HasPrefix(value, "-"):
		operation = SetVarDecrement
		value = strings.TrimPrefix(value, "-")
	}

	return SetVar{
		Collection: collection,
		Name:       name,
		Operation:  operation,
		Value:      value,
	}, nil
}
