package utils

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ReconcileSchema returns the schema to store in state. When schema isn't set in config (null), Terraform
// doesn't manage it: the schema is published outside Terraform, so Cosmo's schema is not copied into state.
// Otherwise the configured value is kept when it differs from Cosmo's only by surrounding whitespace.
func ReconcileSchema(configured types.String, serverSchema string) types.String {
	if configured.IsNull() || serverSchema == "" {
		return configured
	}
	return reconcileTrimmed(configured, serverSchema)
}

// ReconcileReadme returns the readme to store in state. Cosmo strips surrounding whitespace (such as the
// trailing newline of a heredoc) from readmes, so the configured or prior value is kept when it differs
// from Cosmo's only by that whitespace. Otherwise Terraform reports "Provider produced inconsistent result
// after apply", and every plan shows a readme change.
func ReconcileReadme(current types.String, serverReadme string) types.String {
	return reconcileTrimmed(current, serverReadme)
}

func reconcileTrimmed(current types.String, server string) types.String {
	if strings.TrimSpace(current.ValueString()) == strings.TrimSpace(server) {
		return current
	}
	return types.StringValue(server)
}

func StringValueOrNil(s types.String) *string {
	if s.IsNull() {
		return nil
	}
	value := s.ValueString()
	return &value
}

func ConvertHeadersToStringList(headersList types.List) []string {
	var headers []string
	for _, header := range headersList.Elements() {
		if headerStr, ok := header.(types.String); ok {
			headers = append(headers, headerStr.ValueString())
		}
	}
	return headers
}

func GetValueOrDefault[T any](value *T, defaultValue T) T {
	if value == nil {
		return defaultValue
	}
	return *value
}

// NamespaceOrDefault falls back to the "default" namespace. Data source schemas cannot declare defaults.
func NamespaceOrDefault(namespace types.String) types.String {
	if namespace.IsNull() || namespace.ValueString() == "" {
		return types.StringValue("default")
	}
	return namespace
}
