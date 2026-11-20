package utils_test

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

// Plans only, so it needs Terraform but no Cosmo server.
func TestNamespaceNamePlan(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "cosmo" {
  api_url = "http://localhost:1"
  api_key = "unused"
}

resource "cosmo_subgraph" "test" {
  name        = "subgraph"
  namespace   = "11111111-1111-4111-8111-111111111111"
  routing_url = "http://localhost:4000"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`namespace must be the namespace name, not\s+its\s+ID`),
			},
		},
	})
}
