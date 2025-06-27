package comparison

import (
	"testing"
)

func TestComparisonResult_String(t *testing.T) {
	tests := []struct {
		name     string
		result   ComparisonResult
		expected string
	}{
		{
			name:     "equal result",
			result:   ComparisonResult{IsEqual: true},
			expected: "ComparisonResult: IsEqual=true",
		},
		{
			name:     "not equal with nil reason",
			result:   ComparisonResult{IsEqual: false, Reason: nil},
			expected: "ComparisonResult: IsEqual=false, Reason=nil",
		},
		{
			name: "not equal with reason",
			result: ComparisonResult{
				IsEqual: false,
				Reason: &Reason{
					Reason:      "values differ",
					FirstValue:  "a",
					SecondValue: "b",
				},
			},
			expected: "ComparisonResult: IsEqual=false, Reason=values differ, FirstValue=a, SecondValue=b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.result.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestNumberCaster(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int64
	}{
		{"int", int(42), 42},
		{"int8", int8(42), 42},
		{"int16", int16(42), 42},
		{"int32", int32(42), 42},
		{"int64", int64(42), 42},
		{"uint", uint(42), 42},
		{"uint8", uint8(42), 42},
		{"uint16", uint16(42), 42},
		{"uint32", uint32(42), 42},
		{"uint64", uint64(42), 42},
		{"float32", float32(42.7), 42},
		{"float64", float64(42.7), 42},
		{"string", "invalid", -999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := numberCaster(tt.input)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestCompareAny(t *testing.T) {
	tests := []struct {
		name        string
		a           interface{}
		b           interface{}
		expected    bool
		expectError bool
	}{
		{"equal integers", 42, 42, true, false},
		{"different integers", 42, 43, false, false},
		{"int and float equal", 42, 42.0, true, false},
		{"equal strings", "hello", "hello", true, false},
		{"different strings", "hello", "world", false, false},
		{"equal bools", true, true, true, false},
		{"different bools", true, false, false, false},
		{"equal slices", []int{1, 2, 3}, []int{1, 2, 3}, true, false},
		{"different slices", []int{1, 2, 3}, []int{1, 2, 4}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compareAny(tt.a, tt.b)
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCompareExisting(t *testing.T) {
	tests := []struct {
		name        string
		mg          map[string]interface{}
		rm          map[string]interface{}
		expected    bool
		expectError bool
	}{
		{
			name:        "equal maps",
			mg:          map[string]interface{}{"key1": "value1", "key2": 42},
			rm:          map[string]interface{}{"key1": "value1", "key2": 42},
			expected:    true,
			expectError: false,
		},
		{
			name:        "different values",
			mg:          map[string]interface{}{"key1": "value1"},
			rm:          map[string]interface{}{"key1": "value2"},
			expected:    false,
			expectError: false,
		},
		{
			name:        "missing key in rm",
			mg:          map[string]interface{}{"key1": "value1", "key2": "value2"},
			rm:          map[string]interface{}{"key1": "value1"},
			expected:    true,
			expectError: false,
		},
		{
			name:        "different types",
			mg:          map[string]interface{}{"key1": "value1"},
			rm:          map[string]interface{}{"key1": 42},
			expected:    false,
			expectError: true,
		},
		{
			name: "nested maps equal",
			mg: map[string]interface{}{
				"nested": map[string]interface{}{"inner": "value"},
			},
			rm: map[string]interface{}{
				"nested": map[string]interface{}{"inner": "value"},
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "nested maps different",
			mg: map[string]interface{}{
				"nested": map[string]interface{}{"inner": "value1"},
			},
			rm: map[string]interface{}{
				"nested": map[string]interface{}{"inner": "value2"},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "equal slices",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c"},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c"},
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "different slices",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c"},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{"a", "b", "d"},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "slice with maps equal",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value1"},
					map[string]interface{}{"key": "value2"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value1"},
					map[string]interface{}{"key": "value2"},
				},
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "slice with maps different",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value1"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value2"},
				},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "different numeric types but equal values",
			mg: map[string]interface{}{
				"value": int64(123),
			},
			rm: map[string]interface{}{
				"value": int(123),
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "different numeric types but equal values (2)",
			mg: map[string]interface{}{
				"value": int64(123),
			},
			rm: map[string]interface{}{
				"value": float64(123),
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "slice with different lengths",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b"},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c"},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "slice with different maps",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"id": 1, "val": "a"},
					map[string]interface{}{"id": 2, "val": "b"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"id": 1, "val": "a"},
					map[string]interface{}{"id": 3, "val": "c"},
				},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "maps with nil value",
			mg: map[string]interface{}{
				"key1": "value1",
				"key2": nil,
			},
			rm: map[string]interface{}{
				"key1": "value1",
				"key2": nil,
			},
			expected:    true,
			expectError: false,
		},
		{
			name: "both maps with all nil values",
			mg: map[string]interface{}{
				"key1": nil,
				"key2": nil,
			},
			rm: map[string]interface{}{
				"key1": nil,
				"key2": nil,
			},
			expected:    true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CompareExisting(tt.mg, tt.rm)
			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result.IsEqual != tt.expected {
				t.Errorf("expected IsEqual=%v, got IsEqual=%v", tt.expected, result.IsEqual)
			}
		})
	}
}

func TestCompareExisting_EmptyMaps(t *testing.T) {
	mg := map[string]interface{}{}
	rm := map[string]interface{}{}

	result, err := CompareExisting(mg, rm)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result.IsEqual {
		t.Error("expected empty maps to be equal")
	}
}

func TestCompareExisting_SliceTypeAssertionFailure(t *testing.T) {
	mg := map[string]interface{}{
		"slice": []string{"a", "b"}, // Different slice type
	}
	rm := map[string]interface{}{
		"slice": []interface{}{"a", "b"},
	}

	result, err := CompareExisting(mg, rm)
	if err == nil {
		t.Error("expected error for slice type assertion failure")
	}
	if result.IsEqual {
		t.Error("expected IsEqual=false for type assertion failure")
	}
}
