package organization_group_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

// Requires a Cosmo organization with the RBAC feature enabled.
func TestAccOrganizationGroupResource(t *testing.T) {
	name := acctest.RandomWithPrefix("test-group")
	namespace := acctest.RandomWithPrefix("test-namespace")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationGroupConfig(name, "initial", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_organization_group.test", "name", name),
					resource.TestCheckResourceAttr("cosmo_organization_group.test", "description", "initial"),
					resource.TestCheckResourceAttr("cosmo_organization_group.test", "rules.#", "1"),
					resource.TestCheckTypeSetElemNestedAttrs("cosmo_organization_group.test", "rules.*", map[string]string{
						"role": "organization-viewer",
					}),
					resource.TestCheckResourceAttrSet("cosmo_organization_group.test", "id"),
				),
			},
			{
				Config: testAccOrganizationGroupConfig(name, "updated", namespace),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_organization_group.test", "description", "updated"),
					resource.TestCheckResourceAttr("cosmo_organization_group.test", "rules.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs("cosmo_organization_group.test", "rules.*", map[string]string{
						"role":         "namespace-admin",
						"namespaces.#": "1",
					}),
				),
			},
			{
				ResourceName:      "cosmo_organization_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "cosmo_organization_group.test",
				ImportState:       true,
				ImportStateId:     name,
				ImportStateVerify: true,
			},
			{
				Config:  testAccOrganizationGroupConfig(name, "updated", namespace),
				Destroy: true,
			},
		},
	})
}

func TestAccOrganizationGroupDataSource(t *testing.T) {
	name := acctest.RandomWithPrefix("test-group")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccOrganizationGroupConfig(name, "from data source", "") + `
data "cosmo_organization_group" "test" {
  name = cosmo_organization_group.test.name
}

data "cosmo_organization_group" "admin" {
  name = "admin"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.cosmo_organization_group.test", "id", "cosmo_organization_group.test", "id"),
					resource.TestCheckResourceAttr("data.cosmo_organization_group.test", "description", "from data source"),
					resource.TestCheckResourceAttr("data.cosmo_organization_group.test", "builtin", "false"),
					resource.TestCheckResourceAttr("data.cosmo_organization_group.admin", "builtin", "true"),
				),
			},
		},
	})
}

// Validation runs before any API call, so this needs no Cosmo instance.
func TestOrganizationGroupResourceValidation(t *testing.T) {
	t.Setenv("COSMO_API_KEY", "cosmo_validation_only")

	cases := map[string]struct {
		rules string
		error string
	}{
		"organization role with namespaces": {
			rules: `{ role = "organization-admin", namespaces = ["x"] }`,
			error: `applies to the whole organization`,
		},
		"namespaces and resources": {
			rules: `{ role = "graph-admin", namespaces = ["x"], resources = ["y"] }`,
			error: `sets both namespaces and resources`,
		},
		"duplicate role": {
			rules: `{ role = "graph-viewer", resources = ["x"] }, { role = "graph-viewer", resources = ["y"] }`,
			error: `appears in more than one rule`,
		},
		"unknown role": {
			rules: `{ role = "superuser" }`,
			error: `value must be one of`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(`
resource "cosmo_organization_group" "test" {
  name  = "validation"
  rules = [%s]
}
`, tc.rules),
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.error),
					},
				},
			})
		})
	}
}

// testAccOrganizationGroupConfig always grants organization-viewer; when namespace is
// set it also creates that namespace and grants namespace-admin on it.
func testAccOrganizationGroupConfig(name, description, namespace string) string {
	if namespace == "" {
		return fmt.Sprintf(`
resource "cosmo_organization_group" "test" {
  name        = "%s"
  description = "%s"
  rules = [
    { role = "organization-viewer" },
  ]
}
`, name, description)
	}

	return fmt.Sprintf(`
resource "cosmo_namespace" "test" {
  name = "%s"
}

resource "cosmo_organization_group" "test" {
  name        = "%s"
  description = "%s"
  rules = [
    { role = "organization-viewer" },
    { role = "namespace-admin", namespaces = [cosmo_namespace.test.id] },
  ]
}
`, namespace, name, description)
}
