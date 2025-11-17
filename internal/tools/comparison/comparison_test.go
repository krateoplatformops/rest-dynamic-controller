package comparison

import (
	"strings"
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
		{"equal floats", 3.14, 3.14, true, false},
		{"different floats", 3.14, 2.71, false, false},
		{"float equal with different precision", float64(42.7), float32(42.7), true, false},
		{"int and float different", 42, 42.1, false, false},
		{"equal booleans", true, true, true, false},
		{"equal bools", true, true, true, false},
		{"different bools", true, false, false, false},
		{"equal slices", []int{1, 2, 3}, []int{1, 2, 3}, true, false},
		{"different slices", []int{1, 2, 3}, []int{1, 2, 4}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareAny(tt.a, tt.b)
			if tt.expectError {
				t.Error("expected error but got none")
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
			expectError: false,
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
			name: "slices with same strings in different order",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c"},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{"c", "b", "a"},
			},
			expected:    false,
			expectError: false,
		},
		{
			name: "slice with same maps in different order",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"id": 1, "val": "a"},
					map[string]interface{}{"id": 2, "val": "b"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"id": 2, "val": "b"},
					map[string]interface{}{"id": 1, "val": "a"},
				},
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
			name: "different numeric types but equal values (3)",
			mg: map[string]interface{}{
				"value": int64(123),
			},
			rm: map[string]interface{}{
				"value": float64(123.4),
			},
			expected:    false,
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
			expected:    true, // this is considered true because the first map has all keys present in the second map
			expectError: false,
		},
		{
			name: "slice with different lengths",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b", "d"},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{"a", "b", "c", "e"},
			},
			expected:    false, // this is considered false because the first map does not have all keys present in the second map
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
func TestCompareExisting_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		mg          map[string]interface{}
		rm          map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "map type assertion failure in mg",
			mg: map[string]interface{}{
				"nested": map[int]string{1: "invalid"}, // Not map[string]interface{}
			},
			rm: map[string]interface{}{
				"nested": map[string]interface{}{"key": "value"},
			},
			expectError: true,
			errorMsg:    "type assertion failed for map",
		},
		{
			name: "map type assertion failure in rm",
			mg: map[string]interface{}{
				"nested": map[string]interface{}{"key": "value"},
			},
			rm: map[string]interface{}{
				"nested": map[int]string{1: "invalid"}, // Not map[string]interface{}
			},
			expectError: true,
			errorMsg:    "type assertion failed for map",
		},
		{
			name: "slice type assertion failure - different types",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b"},
			},
			rm: map[string]interface{}{
				"slice": "not a slice",
			},
			expectError: true,
			errorMsg:    "values are not both slices",
		},
		{
			name: "slice type assertion failure in rm",
			mg: map[string]interface{}{
				"slice": []interface{}{"a", "b"},
			},
			rm: map[string]interface{}{
				"slice": []string{"a", "b"}, // Not []interface{}
			},
			expectError: true,
			errorMsg:    "type assertion failed for slice",
		},
		{
			name: "slice with map type assertion failure in mg",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[int]string{1: "invalid"}, // Not map[string]interface{}
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value"},
				},
			},
			expectError: true,
			errorMsg:    "type assertion failed for map",
		},
		{
			name: "slice with map type assertion failure in rm",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[int]string{1: "invalid"}, // Not map[string]interface{}
				},
			},
			expectError: true,
			errorMsg:    "type assertion failed for map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CompareExisting(tt.mg, tt.rm)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
				if result.IsEqual {
					t.Error("expected result to be not equal when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCompareExisting_SliceIndexOutOfBounds(t *testing.T) {
	tests := []struct {
		name     string
		mg       map[string]interface{}
		rm       map[string]interface{}
		expected bool
	}{
		{
			name: "slice with different lengths - mg longer",
			mg: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value1"},
					map[string]interface{}{"key": "value2"},
				},
			},
			rm: map[string]interface{}{
				"slice": []interface{}{
					map[string]interface{}{"key": "value1"},
				},
			},
			expected: false, // This should cause an index out of bounds issue
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test should either handle the case gracefully or panic
			// depending on the implementation
			defer func() {
				if r := recover(); r != nil {
					// If it panics, that's a bug that should be fixed
					t.Errorf("Function panicked: %v", r)
				}
			}()

			result, err := CompareExisting(tt.mg, tt.rm)
			if err != nil {
				// Error is acceptable in this edge case
				t.Logf("Got expected error for edge case: %v", err)
			} else {
				if result.IsEqual != tt.expected {
					t.Errorf("expected IsEqual=%v, got IsEqual=%v", tt.expected, result.IsEqual)
				}
			}
		})
	}
}

func TestCompareExisting_NilHandling(t *testing.T) {
	tests := []struct {
		name     string
		mg       map[string]interface{}
		rm       map[string]interface{}
		expected bool
	}{
		{
			name: "one value nil, other not nil",
			mg: map[string]interface{}{
				"key": nil,
			},
			rm: map[string]interface{}{
				"key": "not nil",
			},
			expected: false,
		},
		{
			name: "first value not nil, second nil",
			mg: map[string]interface{}{
				"key": "not nil",
			},
			rm: map[string]interface{}{
				"key": nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CompareExisting(tt.mg, tt.rm)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result.IsEqual != tt.expected {
				t.Errorf("expected IsEqual=%v, got IsEqual=%v", tt.expected, result.IsEqual)
			}
		})
	}
}

func TestCompareExisting_NestedErrors(t *testing.T) {
	// Create a scenario where nested comparison returns an error
	mg := map[string]interface{}{
		"nested": map[string]interface{}{
			"inner": map[int]string{1: "invalid"}, // This will cause type assertion to fail
		},
	}
	rm := map[string]interface{}{
		"nested": map[string]interface{}{
			"inner": map[string]interface{}{"key": "value"},
		},
	}

	result, err := CompareExisting(mg, rm)
	if err == nil {
		t.Error("expected error from nested comparison")
	}
	if result.IsEqual {
		t.Error("expected result to be not equal when nested error occurs")
	}
	if result.Reason == nil {
		t.Error("expected reason to be set when error occurs")
	}
	if result.Reason.Reason != "error comparing maps" {
		t.Errorf("expected reason to be 'error comparing maps', got %q", result.Reason.Reason)
	}
}

func TestCompareExisting_SliceNestedErrors(t *testing.T) {
	// Create a scenario where slice nested comparison returns an error
	mg := map[string]interface{}{
		"slice": []interface{}{
			map[string]interface{}{
				"inner": map[int]string{1: "invalid"}, // This will cause type assertion to fail
			},
		},
	}
	rm := map[string]interface{}{
		"slice": []interface{}{
			map[string]interface{}{
				"inner": map[string]interface{}{"key": "value"},
			},
		},
	}

	result, err := CompareExisting(mg, rm)
	if err == nil {
		t.Error("expected error from nested slice comparison")
	}
	if result.IsEqual {
		t.Error("expected result to be not equal when nested error occurs")
	}
	if result.Reason == nil {
		t.Error("expected reason to be set when error occurs")
	}
	if result.Reason.Reason != "error comparing maps" {
		t.Errorf("expected reason to be 'error comparing maps', got %q", result.Reason.Reason)
	}
}

func TestCompareAny_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{
			name:     "nil values",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "one nil, one not nil",
			a:        nil,
			b:        "not nil",
			expected: false,
		},
		{
			name:     "complex types",
			a:        []interface{}{1, 2, 3},
			b:        []interface{}{1, 2, 3},
			expected: true,
		},
		{
			name:     "different complex types",
			a:        []interface{}{1, 2, 3},
			b:        []interface{}{1, 2, 4},
			expected: false,
		},
		{
			name:     "map comparison",
			a:        map[string]interface{}{"key": "value"},
			b:        map[string]interface{}{"key": "value"},
			expected: true,
		},
		{
			name:     "different maps",
			a:        map[string]interface{}{"key": "value1"},
			b:        map[string]interface{}{"key": "value2"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareAny(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
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

func TestDeepEqual(t *testing.T) {
	tests := []struct {
		name string
		a    interface{}
		b    interface{}
		want bool
	}{
		// Primary Types & Normalization
		{name: "equal integers", a: 42, b: 42, want: true},
		{name: "different integers", a: 42, b: 43, want: false},
		{name: "equal floats", a: 3.14, b: 3.14, want: true},
		{name: "equal strings", a: "hello", b: "hello", want: true},
		{name: "different strings", a: "hello", b: "world", want: false},
		{name: "equal booleans", a: true, b: true, want: true},
		{name: "nil vs nil", a: nil, b: nil, want: true},
		{name: "nil vs non-nil", a: nil, b: 42, want: false},
		// For the following we need to decide on the desired behavior
		//{name: "int vs float64 (equal)", a: 42, b: 42.0, want: true},
		//{name: "int64 vs float64 (equal)", a: int64(42), b: 42.0, want: true},
		//{name: "int vs float32 (equal)", a: 42, b: float32(42.0), want: true},
		{name: "int vs float (unequal)", a: 42, b: 42.1, want: false},

		// Slices (will use direct cmp.Equal)
		{name: "equal int slices", a: []int{1, 2, 3}, b: []int{1, 2, 3}, want: true},
		{name: "unequal int slices (content)", a: []int{1, 2, 3}, b: []int{1, 2, 4}, want: false},
		{name: "unequal int slices (order)", a: []int{1, 2, 3}, b: []int{3, 2, 1}, want: false},
		{name: "equal interface slices", a: []interface{}{1, "a"}, b: []interface{}{1, "a"}, want: true},
		{name: "int vs float in slices", a: []interface{}{1, "a"}, b: []interface{}{1.0, "a"}, want: false},

		// Maps (will use direct cmp.Equal)
		{name: "equal maps", a: map[string]int{"a": 1}, b: map[string]int{"a": 1}, want: true},
		{name: "equal maps (different key order)", a: map[string]int{"a": 1, "b": 2}, b: map[string]int{"b": 2, "a": 1}, want: true},
		{name: "unequal maps (value)", a: map[string]int{"a": 1}, b: map[string]int{"a": 2}, want: false},
		{name: "unequal maps (key)", a: map[string]int{"a": 1}, b: map[string]int{"c": 1}, want: false},
		{name: "int vs float in maps", a: map[string]interface{}{"a": 1}, b: map[string]interface{}{"a": 1.0}, want: false},

		// Nested Structures (will use direct cmp.Equal)
		{name: "equal nested maps", a: map[string]interface{}{"data": map[string]int{"a": 1}}, b: map[string]interface{}{"data": map[string]int{"a": 1}}, want: true},
		{name: "equal map with slice", a: map[string]interface{}{"data": []int{1, 2}}, b: map[string]interface{}{"data": []int{1, 2}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeepEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("DeepEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
