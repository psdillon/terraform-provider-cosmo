package utils

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestNamespaceNameValidator(t *testing.T) {
	tests := []struct {
		name      string
		value     types.String
		wantError bool
	}{
		{name: "name", value: types.StringValue("dev")},
		{name: "name with hyphens", value: types.StringValue("team-a-dev")},
		{name: "default", value: types.StringValue("default")},
		{name: "null", value: types.StringNull()},
		{name: "unknown", value: types.StringUnknown()},
		{name: "id", value: types.StringValue("3f2b9a4e-1c7d-4e8a-9b6f-2d5c8e1a7b40"), wantError: true},
		{name: "upper-case id", value: types.StringValue("3F2B9A4E-1C7D-4E8A-9B6F-2D5C8E1A7B40"), wantError: true},
		{name: "id without hyphens is a name", value: types.StringValue("3f2b9a4e1c7d4e8a9b6f2d5c8e1a7b40")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validator.StringRequest{Path: path.Root("namespace"), ConfigValue: tt.value}
			resp := &validator.StringResponse{}

			NamespaceName().ValidateString(context.Background(), req, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("HasError() = %v, want %v (diagnostics: %v)", got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}
