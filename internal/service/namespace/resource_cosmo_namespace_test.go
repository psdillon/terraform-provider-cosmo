package namespace_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

func TestAccNamespaceResource(t *testing.T) {
	rName := acctest.RandomWithPrefix("test-namespace")
	updatedName := acctest.RandomWithPrefix("updated-namespace")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNamespaceResourceConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_namespace.test", "name", rName),
				),
			},
			{
				ResourceName: "cosmo_namespace.test",
				RefreshState: true,
			},
			{
				Config: testAccNamespaceResourceConfig(updatedName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cosmo_namespace.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_namespace.test", "name", updatedName),
				),
			},
			{
				Config:  testAccNamespaceResourceConfig(rName),
				Destroy: true,
			},
		},
	})
}

func testAccNamespaceResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "cosmo_namespace" "test" {
  name = "%s"
}
`, name)
}

// Proposals need a Cosmo plan that includes them, so this test only runs when
// COSMO_ACC_PROPOSALS=1 is set.
func TestAccNamespaceResourceProposals(t *testing.T) {
	if os.Getenv("COSMO_ACC_PROPOSALS") != "1" {
		t.Skip("set COSMO_ACC_PROPOSALS=1 to test proposals (needs a Cosmo plan with proposals)")
	}

	rName := acctest.RandomWithPrefix("test-namespace")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccNamespaceResourceProposalsConfig(rName, "{}"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_namespace.test", "proposals.check_severity_level", "error"),
					resource.TestCheckResourceAttr("cosmo_namespace.test", "proposals.publish_severity_level", "error"),
				),
			},
			{
				Config: testAccNamespaceResourceProposalsConfig(rName, `{
    check_severity_level   = "warn"
    publish_severity_level = "error"
  }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cosmo_namespace.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_namespace.test", "proposals.check_severity_level", "warn"),
					resource.TestCheckResourceAttr("cosmo_namespace.test", "proposals.publish_severity_level", "error"),
				),
			},
			{
				ResourceName:      "cosmo_namespace.test",
				ImportState:       true,
				ImportStateId:     rName,
				ImportStateVerify: true,
			},
			{
				Config: testAccNamespaceResourceConfig(rName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cosmo_namespace.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("cosmo_namespace.test", "proposals"),
				),
			},
		},
	})
}

func testAccNamespaceResourceProposalsConfig(name, proposals string) string {
	return fmt.Sprintf(`
resource "cosmo_namespace" "test" {
  name      = "%s"
  proposals = %s
}
`, name, proposals)
}
