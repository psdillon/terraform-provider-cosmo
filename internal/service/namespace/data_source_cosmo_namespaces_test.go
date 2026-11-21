package namespace_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1/platformv1connect"

	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
)

const tobeID = "11111111-1111-4111-8111-111111111111"

// namespacesServer answers GetNamespaces over HTTP, the way Cosmo does.
type namespacesServer struct {
	platformv1connect.UnimplementedPlatformServiceHandler

	namespaces []*platformv1.Namespace
	code       common.EnumStatusCode
}

func (s namespacesServer) GetNamespaces(_ context.Context, _ *connect.Request[platformv1.GetNamespacesRequest]) (*connect.Response[platformv1.GetNamespacesResponse], error) {
	return connect.NewResponse(&platformv1.GetNamespacesResponse{
		Response:   &platformv1.Response{Code: s.code, Details: ptr("not allowed")},
		Namespaces: s.namespaces,
	}), nil
}

func ptr(s string) *string { return &s }

func startNamespacesServer(t *testing.T, s namespacesServer) string {
	t.Helper()
	path, handler := platformv1connect.NewPlatformServiceHandler(s)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fmt.Sprintf(`
provider "cosmo" {
  api_url = %q
  api_key = "test-key"
}
`, server.URL)
}

func TestNamespacesDataSource(t *testing.T) {
	provider := startNamespacesServer(t, namespacesServer{
		code: common.EnumStatusCode_OK,
		// Not in name order, to check the data source sorts them.
		namespaces: []*platformv1.Namespace{
			{Id: "22222222-2222-4222-8222-222222222222", Name: "default"},
			{Id: tobeID, Name: "tobe"},
			{Id: "33333333-3333-4333-8333-333333333333", Name: "asis"},
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: provider + `
data "cosmo_namespaces" "all" {}

locals {
  tobe_namespace_id = one([for ns in data.cosmo_namespaces.all.namespaces : ns.id if ns.name == "tobe"])
}

output "tobe_namespace_id" {
  value = local.tobe_namespace_id
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckOutput("tobe_namespace_id", tobeID),
					resource.TestCheckResourceAttr("data.cosmo_namespaces.all", "namespaces.#", "3"),
					resource.TestCheckResourceAttr("data.cosmo_namespaces.all", "namespaces.0.name", "asis"),
					resource.TestCheckResourceAttr("data.cosmo_namespaces.all", "namespaces.1.name", "default"),
					resource.TestCheckResourceAttr("data.cosmo_namespaces.all", "namespaces.2.name", "tobe"),
					resource.TestCheckResourceAttr("data.cosmo_namespaces.all", "namespaces.2.id", tobeID),
				),
			},
		},
	})
}

func TestNamespacesDataSourceError(t *testing.T) {
	provider := startNamespacesServer(t, namespacesServer{code: common.EnumStatusCode_ERROR_NOT_AUTHORIZED})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      provider + `data "cosmo_namespaces" "all" {}`,
				ExpectError: regexp.MustCompile(`Error Reading Namespaces`),
			},
		},
	})
}

func TestAccNamespacesDataSource(t *testing.T) {
	rName := acctest.RandomWithPrefix("test-namespace")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "cosmo_namespace" "test" {
  name = %q
}

data "cosmo_namespaces" "all" {
  depends_on = [cosmo_namespace.test]
}

output "test_namespace_id" {
  value = one([for ns in data.cosmo_namespaces.all.namespaces : ns.id if ns.name == cosmo_namespace.test.name])
}
`, rName),
				Check: func(s *terraform.State) error {
					want := s.RootModule().Resources["cosmo_namespace.test"].Primary.ID
					output, ok := s.RootModule().Outputs["test_namespace_id"]
					if !ok || output.Value != want {
						return fmt.Errorf("test_namespace_id = %v, want %q", output, want)
					}
					return nil
				},
			},
		},
	})
}
