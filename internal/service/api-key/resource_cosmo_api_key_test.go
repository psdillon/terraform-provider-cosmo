package api_key_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

// Requires a Cosmo organization with the RBAC feature enabled.
func TestAccApiKeyResource(t *testing.T) {
	name := acctest.RandomWithPrefix("test-key")
	groupA := acctest.RandomWithPrefix("test-group-a")
	groupB := acctest.RandomWithPrefix("test-group-b")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApiKeyConfig(name, groupA, groupB, "a", "30_days"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_api_key.test", "name", name),
					resource.TestCheckResourceAttr("cosmo_api_key.test", "expires", "30_days"),
					resource.TestCheckResourceAttrPair("cosmo_api_key.test", "group_id", "cosmo_organization_group.a", "id"),
					resource.TestMatchResourceAttr("cosmo_api_key.test", "key", regexp.MustCompile(`^cosmo_`)),
					resource.TestCheckResourceAttrSet("cosmo_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("cosmo_api_key.test", "expires_at"),
				),
			},
			{
				// Moving the key to another group updates it in place and keeps the secret.
				Config: testAccApiKeyConfig(name, groupA, groupB, "b", "30_days"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cosmo_api_key.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("cosmo_api_key.test", "group_id", "cosmo_organization_group.b", "id"),
					resource.TestMatchResourceAttr("cosmo_api_key.test", "key", regexp.MustCompile(`^cosmo_`)),
				),
			},
			{
				ResourceName:                         "cosmo_api_key.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
				ImportStateVerifyIgnore:              []string{"key", "permissions"},
			},
			{
				Config:  testAccApiKeyConfig(name, groupA, groupB, "b", "30_days"),
				Destroy: true,
			},
		},
	})
}

func testAccApiKeyConfig(name, groupA, groupB, useGroup, expires string) string {
	return fmt.Sprintf(`
resource "cosmo_organization_group" "a" {
  name  = "%s"
  rules = [{ role = "organization-viewer" }]
}

resource "cosmo_organization_group" "b" {
  name  = "%s"
  rules = [{ role = "organization-developer" }]
}

resource "cosmo_api_key" "test" {
  name     = "%s"
  group_id = cosmo_organization_group.%s.id
  expires  = "%s"
}
`, groupA, groupB, name, useGroup, expires)
}
