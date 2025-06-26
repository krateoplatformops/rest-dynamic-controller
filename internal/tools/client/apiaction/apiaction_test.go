package apiaction

import "testing"

func TestAPIAction_String(t *testing.T) {
	tests := []struct {
		name     string
		action   APIAction
		expected string
	}{
		{
			name:     "Create action",
			action:   Create,
			expected: "create",
		},
		{
			name:     "Update action",
			action:   Update,
			expected: "update",
		},
		{
			name:     "Delete action",
			action:   Delete,
			expected: "delete",
		},
		{
			name:     "List action",
			action:   List,
			expected: "list",
		},
		{
			name:     "Get action",
			action:   Get,
			expected: "get",
		},
		{
			name:     "FindBy action",
			action:   FindBy,
			expected: "findby",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.action.String()
			if result != tt.expected {
				t.Errorf("APIAction.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAPIActionConstants(t *testing.T) {
	tests := []struct {
		name     string
		action   APIAction
		expected string
	}{
		{"Create constant", Create, "create"},
		{"Update constant", Update, "update"},
		{"Delete constant", Delete, "delete"},
		{"List constant", List, "list"},
		{"Get constant", Get, "get"},
		{"FindBy constant", FindBy, "findby"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.action) != tt.expected {
				t.Errorf("APIAction constant %v = %v, want %v", tt.name, string(tt.action), tt.expected)
			}
		})
	}
}
