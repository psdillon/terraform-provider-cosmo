package namespace_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

// These only plan a new namespace, so they need Terraform but no Cosmo server.
const planOnlyProvider = `
provider "cosmo" {
  api_url = "http://localhost:1"
  api_key = "unused"
}
`

func TestNamespaceProposalsPlan(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: planOnlyProvider + `
resource "cosmo_namespace" "test" {
  name      = "ns"
  proposals = { check_severity_level = "warn" }
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue("cosmo_namespace.test", tfjsonpath.New("proposals"), knownvalue.ObjectExact(map[string]knownvalue.Check{
							"check_severity_level":   knownvalue.StringExact("warn"),
							"publish_severity_level": knownvalue.StringExact("error"),
						})),
					},
				},
			},
			{
				Config: planOnlyProvider + `
resource "cosmo_namespace" "test" {
  name = "ns"
}
`,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectKnownValue("cosmo_namespace.test", tfjsonpath.New("proposals"), knownvalue.Null()),
					},
				},
			},
			{
				Config: planOnlyProvider + `
resource "cosmo_namespace" "test" {
  name      = "ns"
  proposals = { publish_severity_level = "fatal" }
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}
