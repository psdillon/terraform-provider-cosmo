package contract_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/acceptance"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// A contract on a graph with no subgraphs is created with the help of a temporary subgraph, which
// is gone afterwards.
func TestAccContractResourceTempSubgraph(t *testing.T) {
	namespace := acctest.RandomWithPrefix("test-namespace")
	graphName := acctest.RandomWithPrefix("test-federated-graph")
	contractName := acctest.RandomWithPrefix("test-contract")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccContractTempSubgraphConfig(namespace, graphName, contractName, false, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cosmo_contract.test", "name", contractName),
					resource.TestCheckResourceAttr("cosmo_contract.test", "create_temp_subgraph_if_needed", "true"),
					resource.TestCheckResourceAttrSet("cosmo_contract.test", "id"),
					testAccCheckTempSubgraphDeleted(namespace, "temp-contract-bootstrap-"+contractName),
				),
			},
		},
	})
}

// With another graph that would pick up the temporary subgraph, the default is to refuse and
// change nothing; allow_recomposing_other_graphs = true goes ahead.
func TestAccContractResourceTempSubgraphOtherGraphs(t *testing.T) {
	namespace := acctest.RandomWithPrefix("test-namespace")
	graphName := acctest.RandomWithPrefix("test-federated-graph")
	contractName := acctest.RandomWithPrefix("test-contract")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acceptance.TestAccPreCheck(t) },
		ProtoV6ProviderFactories: acceptance.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccContractTempSubgraphConfig(namespace, graphName, contractName, true, false),
				ExpectError: regexp.MustCompile(`(?s)it would also recompose:.*Nothing was changed`),
			},
			{
				Config: testAccContractTempSubgraphConfig(namespace, graphName, contractName, true, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cosmo_contract.test", "id"),
					testAccCheckTempSubgraphDeleted(namespace, "temp-contract-bootstrap-"+contractName),
				),
			},
		},
	})
}

func testAccCheckTempSubgraphDeleted(namespace, subgraphName string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		client, err := api.NewClient("", "")
		if err != nil {
			return err
		}
		_, apiError := client.GetSubgraph(context.Background(), subgraphName, namespace)
		if apiError == nil {
			return fmt.Errorf("temporary subgraph %q still exists", subgraphName)
		}
		if !api.IsNotFoundError(apiError) {
			return fmt.Errorf("checking for temporary subgraph %q: %s", subgraphName, apiError.Error())
		}
		return nil
	}
}

func testAccContractTempSubgraphConfig(namespace, graphName, contractName string, withOtherGraph, allowOthers bool) string {
	// Matches the same labels as the source graph, so it would pick up the temporary subgraph.
	var otherGraph string
	if withOtherGraph {
		otherGraph = fmt.Sprintf(`
resource "cosmo_federated_graph" "other" {
  name           = "%s-other"
  namespace      = cosmo_namespace.test.name
  routing_url    = "https://example.com:3001"
  label_matchers = ["team=a"]
}
`, graphName)
	}

	return fmt.Sprintf(`
resource "cosmo_namespace" "test" {
  name = "%s"
}

resource "cosmo_federated_graph" "test" {
  name           = "%s"
  namespace      = cosmo_namespace.test.name
  routing_url    = "https://example.com:3000"
  label_matchers = ["team=a"]
}
%s
resource "cosmo_contract" "test" {
  name         = "%s"
  namespace    = cosmo_namespace.test.name
  source       = cosmo_federated_graph.test.name
  routing_url  = "http://localhost:3003"
  include_tags = ["public"]

  create_temp_subgraph_if_needed = true
  allow_recomposing_other_graphs = %t

  depends_on = [cosmo_federated_graph.test%s]
}
`, namespace, graphName, otherGraph, contractName, allowOthers, map[bool]string{true: ", cosmo_federated_graph.other", false: ""}[withOtherGraph])
}
