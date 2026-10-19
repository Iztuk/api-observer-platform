package query

import (
	"observer/internal/audit"
	"testing"
)

func TestEvaluateComparison(t *testing.T) {
	tests := []struct {
		name     string
		expr     ComparisonExpression
		item     LogItem
		expected bool
		wantErr  bool
	}{
		{
			name: "string equal matches",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("host"),
				},
				Operator: Operator{
					Type: OperatorTypeEqual,
				},
				Value: Value{
					Type:  ValueTypeString,
					Value: "test-service",
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Host: "test-service",
				},
			},
			expected: true,
		},
		{
			name: "string equal does not match",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("host"),
				},
				Operator: Operator{
					Type: OperatorTypeEqual,
				},
				Value: Value{
					Type:  ValueTypeString,
					Value: "test-service",
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Host: "other-service",
				},
			},
			expected: false,
		},
		{
			name: "string not equal matches",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("method"),
				},
				Operator: Operator{
					Type: OperatorTypeNotEqual,
				},
				Value: Value{
					Type:  ValueTypeString,
					Value: "POST",
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Method: "GET",
				},
			},
			expected: true,
		},
		{
			name: "string not equal does not match",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("method"),
				},
				Operator: Operator{
					Type: OperatorTypeNotEqual,
				},
				Value: Value{
					Type:  ValueTypeString,
					Value: "GET",
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Method: "GET",
				},
			},
			expected: false,
		},
		{
			name: "number equal matches",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeEqual,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 200,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 200,
				},
			},
			expected: true,
		},
		{
			name: "number greater matches",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeGreater,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 399,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 500,
				},
			},
			expected: true,
		},
		{
			name: "number greater does not match",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeGreater,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 500,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 400,
				},
			},
			expected: false,
		},
		{
			name: "number greater equal matches greater",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeGreaterEqual,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 400,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 500,
				},
			},
			expected: true,
		},
		{
			name: "number greater equal matches equal",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeGreaterEqual,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 400,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 400,
				},
			},
			expected: true,
		},
		{
			name: "number less matches",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeLess,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 400,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 200,
				},
			},
			expected: true,
		},
		{
			name: "number less equal matches equal",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("status"),
				},
				Operator: Operator{
					Type: OperatorTypeLessEqual,
				},
				Value: Value{
					Type:  ValueTypeNumber,
					Value: 400,
				},
			},
			item: LogItem{
				Job: audit.AuditJob{
					Status: 400,
				},
			},
			expected: true,
		},
		{
			name: "unknown field returns error",
			expr: ComparisonExpression{
				Field: Field{
					Name: FieldName("banana"),
				},
				Operator: Operator{
					Type: OperatorTypeEqual,
				},
				Value: Value{
					Type:  ValueTypeString,
					Value: "test",
				},
			},
			item:    LogItem{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluateComparison(
				tt.expr,
				tt.item,
			)

			t.Logf(
				"status=%d operator=%v queryValue=%v result=%v",
				tt.item.Job.Status,
				tt.expr.Operator.Type,
				tt.expr.Value.Value,
				result,
			)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"unexpected error: %v",
					err,
				)
			}

			if result != tt.expected {
				t.Errorf(
					"expected %v, got %v",
					tt.expected,
					result,
				)
			}
		})
	}
}
