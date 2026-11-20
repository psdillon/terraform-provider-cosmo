package organization_group_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

// These only plan, against a Cosmo URL that refuses connections, so listing the namespaces always
// fails. They need Terraform but no Cosmo server.
const planOnlyProvider = `
provider "cosmo" {
  api_url = "http://localhost:1"
  api_key = "unused"
}
`

func TestOrganizationGroupNamespacesPlan(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// A namespace name is rejected during plan, even though the names can't be looked up.
				Config: planOnlyProvider + `
resource "cosmo_namespace" "team" {
  name = "team"
}

resource "cosmo_organization_group" "test" {
  name  = "group"
  rules = [{ role = "namespace-admin", namespaces = [cosmo_namespace.team.name] }]
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`lists "team", which is not a namespace\s+ID`),
			},
			{
				// The ID of a namespace created in the same apply is unknown during plan, so it isn't
				// checked then (and Cosmo isn't called).
				Config: planOnlyProvider + `
resource "cosmo_namespace" "team" {
  name = "team"
}

resource "cosmo_organization_group" "test" {
  name  = "group"
  rules = [{ role = "namespace-admin", namespaces = [cosmo_namespace.team.id] }]
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// An ID that can't be checked only gives a warning.
				Config: planOnlyProvider + `
resource "cosmo_organization_group" "test" {
  name  = "group"
  rules = [{ role = "namespace-admin", namespaces = ["11111111-1111-4111-8111-111111111111"] }]
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
