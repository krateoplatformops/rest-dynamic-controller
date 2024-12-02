package text

import (
	"testing"
)

func TestGenericToString(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected string
		hasError bool
	}{
		{"hello", "hello", false},
		{123, "123", false},
		{123.456, "123", false},
		{true, "true", false},
		{false, "false", false},
		{[]int{1, 2, 3}, "[1,2,3]", false},
	}

	for _, test := range tests {
		result, err := GenericToString(test.input)
		if (err != nil) != test.hasError {
			t.Errorf("GenericToString(%v) returned error: %v, expected error: %v", test.input, err, test.hasError)
		}
		if result != test.expected {
			t.Errorf("GenericToString(%v) = %v, expected %v", test.input, result, test.expected)
		}
	}
}
