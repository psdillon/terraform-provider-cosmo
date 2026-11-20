package utils

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// ErrInvalidNamespaceName is the diagnostic title used when a namespace ID is given where a name is expected.
const ErrInvalidNamespaceName = "Invalid Namespace Name"

// uuidPattern matches the canonical 8-4-4-4-12 form Cosmo uses for IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether s looks like a Cosmo ID.
func IsUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

var _ validator.String = namespaceNameValidator{}

// namespaceNameValidator rejects namespace IDs in attributes that take a namespace name. Cosmo
// looks namespaces up by name, so an ID plans cleanly and only fails on apply with
// "Could not find namespace <id>".
type namespaceNameValidator struct{}

// NamespaceName returns a validator that rejects namespace IDs where a namespace name is expected.
func NamespaceName() validator.String {
	return namespaceNameValidator{}
}

func (v namespaceNameValidator) Description(ctx context.Context) string {
	return "value must be the namespace name, not its ID"
}

func (v namespaceNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v namespaceNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if IsUUID(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			ErrInvalidNamespaceName,
			"namespace must be the namespace name, not its ID. "+
				"Use the namespace's name attribute (for example cosmo_namespace.example.name) instead of its id.",
		)
	}
}
