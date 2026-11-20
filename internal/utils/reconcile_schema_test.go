package utils

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestReconcileSchema(t *testing.T) {
	testCases := []struct {
		name         string
		configured   types.String
		serverSchema string
		expected     types.String
	}{
		{
			name:         "keeps configured value when server only strips trailing whitespace",
			configured:   types.StringValue("type Query {\n  hello: String\n}\n\n"),
			serverSchema: "type Query {\n  hello: String\n}",
			expected:     types.StringValue("type Query {\n  hello: String\n}\n\n"),
		},
		{
			name:         "keeps configured value when server only strips surrounding whitespace",
			configured:   types.StringValue("\ntype Query {\n  hello: String\n}\n"),
			serverSchema: "type Query {\n  hello: String\n}",
			expected:     types.StringValue("\ntype Query {\n  hello: String\n}\n"),
		},
		{
			name:         "adopts server value when the document differs",
			configured:   types.StringValue("type Query {\n  hello: String\n}"),
			serverSchema: "type Query {\n  world: String\n}",
			expected:     types.StringValue("type Query {\n  world: String\n}"),
		},
		{
			name:         "keeps null when schema is not managed by terraform",
			configured:   types.StringNull(),
			serverSchema: "type Query {\n  hello: String\n}",
			expected:     types.StringNull(),
		},
		{
			name:         "keeps configured value when server schema is empty",
			configured:   types.StringValue("type Query {\n  hello: String\n}"),
			serverSchema: "",
			expected:     types.StringValue("type Query {\n  hello: String\n}"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReconcileSchema(tc.configured, tc.serverSchema)
			if !got.Equal(tc.expected) {
				t.Errorf("reconcileSchema() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestReconcileReadme(t *testing.T) {
	testCases := []struct {
		name         string
		current      types.String
		serverReadme string
		expected     types.String
	}{
		{
			name:         "keeps heredoc readme when server strips the trailing newline",
			current:      types.StringValue("# CUDA Subgraph\n\nDetails.\n"),
			serverReadme: "# CUDA Subgraph\n\nDetails.",
			expected:     types.StringValue("# CUDA Subgraph\n\nDetails.\n"),
		},
		{
			name:         "keeps current value when server strips surrounding whitespace",
			current:      types.StringValue("\n  readme  \n"),
			serverReadme: "readme",
			expected:     types.StringValue("\n  readme  \n"),
		},
		{
			name:         "adopts server value when the readme was changed outside terraform",
			current:      types.StringValue("old readme\n"),
			serverReadme: "new readme",
			expected:     types.StringValue("new readme"),
		},
		{
			name:         "adopts server value when current is null (import)",
			current:      types.StringNull(),
			serverReadme: "readme",
			expected:     types.StringValue("readme"),
		},
		{
			name:         "keeps null when server readme is empty",
			current:      types.StringNull(),
			serverReadme: "",
			expected:     types.StringNull(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReconcileReadme(tc.current, tc.serverReadme)
			if !got.Equal(tc.expected) {
				t.Errorf("ReconcileReadme() = %v, want %v", got, tc.expected)
			}
		})
	}
}
