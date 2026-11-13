package contract

// This file implements create_temp_subgraph_if_needed.
//
// Cosmo refuses to create a contract until its source graph has composed successfully, and a
// graph only composes once it has at least one subgraph with a Query field. When a source graph
// has no published subgraphs yet (common when the graph and its contracts are set up before any
// service has published a schema), the contract can't be created.
//
// The workaround: create a clearly named temporary subgraph that matches only the source graph,
// publish a one-field schema to it so the source graph composes, create the contract, then delete
// the temporary subgraph again.
//
// The matching rules below mirror Cosmo's controlplane (FederatedGraphRepository.bySubgraphLabels):
//   - a subgraph with no labels joins only graphs that have no label matchers;
//   - a subgraph with labels joins only graphs that have label matchers, and only if every one of
//     the graph's matchers contains at least one of the subgraph's labels.
// Publishing or deleting a subgraph recomposes every graph it joins, plus those graphs' contracts.
// Creating a subgraph (without a schema) composes nothing.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/utils"
)

const (
	// tempSubgraphPrefix makes the purpose of the subgraph obvious to anyone who finds it in Cosmo.
	tempSubgraphPrefix = "temp-contract-bootstrap-"
	// tempSubgraphRoutingURL uses the reserved .invalid top-level domain, which never resolves.
	tempSubgraphRoutingURL = "http://temp-contract-bootstrap.invalid/graphql"
	// tempSubgraphField is the single Query field published by the temporary subgraph.
	tempSubgraphField = "_tempContractBootstrap"
	// maxGraphNameLength is Cosmo's limit for graph and subgraph names.
	maxGraphNameLength = 100
	// maxLabelCombinations caps the search for the best set of labels.
	maxLabelCombinations = 4096
	// cleanupTimeout bounds the clean-up, which keeps running even if Terraform is interrupted.
	cleanupTimeout = 5 * time.Minute
)

// sourceGraphLocks makes contracts that share a source graph bootstrap one at a time. Terraform
// creates resources in parallel, and two bootstraps of the same graph would otherwise interfere.
var sourceGraphLocks sync.Map

func lockSourceGraph(namespace, sourceGraph string) func() {
	value, _ := sourceGraphLocks.LoadOrStore(namespace+"/"+sourceGraph, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

// tempSubgraphName returns the name of the temporary subgraph for a contract. It is deterministic,
// so a subgraph left behind by an interrupted apply can be found again.
func tempSubgraphName(contractName string) string {
	name := tempSubgraphPrefix + contractName
	if len(name) <= maxGraphNameLength {
		return name
	}

	// Too long for Cosmo: shorten the contract name and add a hash to keep it unique.
	sum := sha256.Sum256([]byte(contractName))
	suffix := "-" + hex.EncodeToString(sum[:])[:8]
	keep := contractName[:maxGraphNameLength-len(tempSubgraphPrefix)-len(suffix)]
	// Cosmo names must not end with a separator.
	keep = strings.TrimRight(keep, "./@_-")
	return tempSubgraphPrefix + keep + suffix
}

// tempSubgraphSchema returns the SDL published to the temporary subgraph. The comments and the
// field description explain to anyone who finds it why it exists and that it can be deleted.
func tempSubgraphSchema(contractName, namespace, subgraphName string, includeTags []string) string {
	var tags strings.Builder
	for _, tag := range includeTags {
		fmt.Fprintf(&tags, " @tag(name: %q)", tag)
	}

	return fmt.Sprintf(`# TEMPORARY SUBGRAPH - SAFE TO DELETE.
#
# Created by the Terraform cosmo provider while creating cosmo_contract %[1]q in namespace %[2]q
# (create_temp_subgraph_if_needed = true).
#
# Cosmo only creates a contract once its source graph has composed successfully, and a graph only
# composes when it has at least one Query field. The source graph had no published subgraphs, so the
# provider published this one-field subgraph, created the contract, and then deleted this subgraph.
#
# If you can see this, the clean-up did not finish (for example, the apply was interrupted).
# It is safe to delete subgraph %[3]q in namespace %[2]q.
extend schema @link(url: "https://specs.apollo.dev/federation/v2.5", import: ["@shareable", "@tag"])

type Query {
  """
  TEMPORARY - created by the Terraform cosmo provider only so that cosmo_contract %[1]q could be
  created. It should have been deleted straight away. If you can see it, it is safe to delete
  subgraph %[3]q in namespace %[2]q. Querying this field always fails.
  """
  %[4]s: String @shareable%[5]s
}
`, contractName, namespace, subgraphName, tempSubgraphField, tags.String())
}

// tempSubgraphReadme must not contain "; delete": Cosmo Cloud's firewall rejects requests containing
// it (it looks like SQL injection) with a bare 403 Forbidden.
func tempSubgraphReadme(contractName, namespace, subgraphName string) string {
	return fmt.Sprintf("TEMPORARY - SAFE TO DELETE. Created by the Terraform cosmo provider so that cosmo_contract %q "+
		"could be created: Cosmo only creates a contract once its source graph composes, and the source graph had no "+
		"published subgraphs. The provider deletes this subgraph as soon as the contract exists. If you can see it, "+
		"the clean-up did not finish. It is safe to delete subgraph %q in namespace %q.", contractName, subgraphName, namespace)
}

// parseLabelMatchers turns Cosmo label matchers ("team=a,team=b") into sets of "key=value" strings.
// Commas within a matcher mean OR; separate matchers mean AND.
func parseLabelMatchers(matchers []string) [][]string {
	var parsed [][]string
	for _, matcher := range matchers {
		var options []string
		seen := map[string]bool{}
		for _, option := range strings.Split(matcher, ",") {
			option = strings.TrimSpace(option)
			if option == "" || seen[option] {
				continue
			}
			seen[option] = true
			options = append(options, option)
		}
		if len(options) > 0 {
			parsed = append(parsed, options)
		}
	}
	return parsed
}

// graphMatchesLabels reports whether Cosmo would add a subgraph with these labels to a graph with
// these label matchers.
func graphMatchesLabels(matchers [][]string, labels []string) bool {
	if len(labels) == 0 {
		return len(matchers) == 0
	}
	if len(matchers) == 0 {
		return false
	}

	labelSet := map[string]bool{}
	for _, label := range labels {
		labelSet[label] = true
	}
	for _, options := range matchers {
		found := false
		for _, option := range options {
			if labelSet[option] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// otherGraph is a non-contract graph in the namespace, other than the source graph.
type otherGraph struct {
	name     string
	matchers [][]string
}

// chooseLabels picks labels that the source graph's matchers accept while matching as few other
// graphs as possible (ideally none), using as few labels as possible.
func chooseLabels(sourceMatchers [][]string, others []otherGraph) []string {
	if len(sourceMatchers) == 0 {
		// A graph without label matchers only takes subgraphs without labels.
		return nil
	}

	countMatches := func(labels []string) int {
		count := 0
		for _, other := range others {
			if graphMatchesLabels(other.matchers, labels) {
				count++
			}
		}
		return count
	}

	combinations := 1
	for _, options := range sourceMatchers {
		combinations *= len(options)
		if combinations > maxLabelCombinations {
			break
		}
	}

	if combinations > maxLabelCombinations {
		// Too many to try them all: take the first option of each matcher.
		var labels []string
		for _, options := range sourceMatchers {
			labels = append(labels, options[0])
		}
		return uniqueSorted(labels)
	}

	var best []string
	bestMatches := -1
	choice := make([]string, len(sourceMatchers))
	var try func(index int)
	try = func(index int) {
		if index == len(sourceMatchers) {
			labels := uniqueSorted(choice)
			matches := countMatches(labels)
			if bestMatches == -1 || matches < bestMatches || (matches == bestMatches && len(labels) < len(best)) {
				best = labels
				bestMatches = matches
			}
			return
		}
		for _, option := range sourceMatchers[index] {
			choice[index] = option
			try(index + 1)
		}
	}
	try(0)

	return best
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

// affectedGraphs lists the graphs and contracts that publishing or deleting a subgraph with these
// labels would recompose, apart from the source graph itself. The contract being created doesn't
// exist yet, so it is never in the list.
func affectedGraphs(source *platformv1.FederatedGraph, graphs []*platformv1.FederatedGraph, labels []string) []string {
	recomposed := map[string]bool{source.GetId(): true}
	var affected []string

	for _, graph := range graphs {
		if graph.GetContract() != nil || graph.GetId() == source.GetId() {
			continue
		}
		if graphMatchesLabels(parseLabelMatchers(graph.GetLabelMatchers()), labels) {
			recomposed[graph.GetId()] = true
			affected = append(affected, fmt.Sprintf("graph %q", graph.GetName()))
		}
	}

	// Contracts are recomposed whenever their source graph is.
	for _, graph := range graphs {
		if graph.GetContract() != nil && recomposed[graph.GetContract().GetSourceFederatedGraphId()] {
			affected = append(affected, fmt.Sprintf("contract %q", graph.GetName()))
		}
	}

	sort.Strings(affected)
	return affected
}

func otherGraphs(source *platformv1.FederatedGraph, graphs []*platformv1.FederatedGraph) []otherGraph {
	var others []otherGraph
	for _, graph := range graphs {
		if graph.GetContract() != nil || graph.GetId() == source.GetId() {
			continue
		}
		others = append(others, otherGraph{name: graph.GetName(), matchers: parseLabelMatchers(graph.GetLabelMatchers())})
	}
	return others
}

func toAPILabels(labels []string) []*platformv1.Label {
	var result []*platformv1.Label
	for _, label := range labels {
		key, value, _ := strings.Cut(label, "=")
		result = append(result, &platformv1.Label{Key: key, Value: value})
	}
	return result
}

func fromAPILabels(labels []*platformv1.Label) []string {
	var result []string
	for _, label := range labels {
		result = append(result, label.GetKey()+"="+label.GetValue())
	}
	return uniqueSorted(result)
}

func hasPublishedSubgraph(subgraphs []*platformv1.Subgraph) bool {
	for _, subgraph := range subgraphs {
		// LastUpdatedAt is the time of the subgraph's latest schema, and empty if it never had one.
		if subgraph.GetLastUpdatedAt() != "" {
			return true
		}
	}
	return false
}

// tempSubgraph is a temporary subgraph that is in place and must be removed once the contract exists.
type tempSubgraph struct {
	name      string
	namespace string
	// affected lists the other graphs and contracts that were (and will be again) recomposed.
	affected []string
}

// setUpTempSubgraph creates and publishes the temporary subgraph if the contract needs one. It
// returns nil, with no diagnostics, when the contract can be created without one. A subgraph left
// behind by an earlier, interrupted apply for this contract is reused, so it gets cleaned up too.
func (r *contractResource) setUpTempSubgraph(ctx context.Context, data contractResourceModel) (*tempSubgraph, diag.Diagnostics) {
	var diags diag.Diagnostics
	contractName := data.Name.ValueString()
	namespace := data.Namespace.ValueString()
	sourceName := data.SourceGraphName.ValueString()
	subgraphName := tempSubgraphName(contractName)

	sourceResponse, apiError := r.client.GetFederatedGraph(ctx, sourceName, namespace)
	if apiError != nil {
		diags.AddError(ErrCreatingContract, fmt.Sprintf("Could not read source graph %q: %s", sourceName, apiError.Error()))
		return nil, diags
	}
	source := sourceResponse.GetGraph()

	var leftover *platformv1.Subgraph
	var realSubgraphs []*platformv1.Subgraph
	for _, subgraph := range sourceResponse.GetSubgraphs() {
		if subgraph.GetName() == subgraphName {
			leftover = subgraph
		} else {
			realSubgraphs = append(realSubgraphs, subgraph)
		}
	}

	if leftover == nil {
		// A leftover that is no longer part of the source graph (for example, because the graph's
		// label matchers changed) can't be reused or deleted without affecting other graphs.
		existing, apiError := r.client.GetSubgraph(ctx, subgraphName, namespace)
		if apiError == nil && existing != nil {
			diags.AddError(ErrCreatingContract, fmt.Sprintf(
				"A temporary subgraph %q from an earlier, interrupted apply exists in namespace %q, but it is no longer "+
					"part of source graph %q. Check which graphs it belongs to, delete it, and run the apply again.",
				subgraphName, namespace, sourceName))
			return nil, diags
		}
		if apiError != nil && !api.IsNotFoundError(apiError) {
			diags.AddError(ErrCreatingContract, fmt.Sprintf("Could not check for subgraph %q: %s", subgraphName, apiError.Error()))
			return nil, diags
		}

		needed := source.GetSupportsFederation() && !source.GetIsComposable() && !hasPublishedSubgraph(realSubgraphs)
		if !needed {
			utils.DebugAction(ctx, DebugCreate, contractName, namespace, map[string]interface{}{
				"temp_subgraph": "not needed",
			})
			return nil, diags
		}
	}

	graphs, apiError := r.client.GetFederatedGraphs(ctx, namespace)
	if apiError != nil {
		diags.AddError(ErrCreatingContract, fmt.Sprintf("Could not list the graphs in namespace %q: %s", namespace, apiError.Error()))
		return nil, diags
	}

	var labels []string
	if leftover != nil {
		labels = fromAPILabels(leftover.GetLabels())
	} else {
		labels = chooseLabels(parseLabelMatchers(source.GetLabelMatchers()), otherGraphs(source, graphs))
	}

	affected := affectedGraphs(source, graphs, labels)
	if len(affected) > 0 && !data.AllowRecomposingOtherGraphs.ValueBool() {
		diags.AddError(ErrCreatingContract, fmt.Sprintf(
			"Source graph %q has not composed yet, so Cosmo won't create the contract. A temporary subgraph would fix "+
				"that, but it would also recompose: %s. Nothing was changed. Publish a real subgraph to %q first, or set "+
				"allow_recomposing_other_graphs = true if recomposing those is acceptable.",
			sourceName, strings.Join(affected, ", "), sourceName))
		return nil, diags
	}

	temp := &tempSubgraph{name: subgraphName, namespace: namespace, affected: affected}

	if leftover == nil {
		readme := tempSubgraphReadme(contractName, namespace, subgraphName)
		routingURL := tempSubgraphRoutingURL
		apiError = r.client.CreateSubgraph(ctx, &platformv1.CreateFederatedSubgraphRequest{
			Name:       subgraphName,
			Namespace:  namespace,
			RoutingUrl: &routingURL,
			Labels:     toAPILabels(labels),
			Readme:     &readme,
		})
		if apiError != nil {
			diags.AddError(ErrCreatingContract, fmt.Sprintf("Could not create temporary subgraph %q: %s", subgraphName, apiError.Error()))
			return nil, diags
		}
	}

	utils.DebugAction(ctx, DebugCreate, contractName, namespace, map[string]interface{}{
		"temp_subgraph": subgraphName,
		"labels":        strings.Join(labels, ","),
		"affected":      strings.Join(affected, ", "),
		"reused":        leftover != nil,
	})

	includeTags, err := utils.ConvertLabelMatchers(data.IncludeTags)
	if err != nil {
		diags.Append(r.removeTempSubgraph(ctx, temp)...)
		diags.AddError(ErrCreatingContract, "Could not read include_tags: "+err.Error())
		return nil, diags
	}

	// Publish even when reusing a leftover, so its schema matches this contract's include_tags.
	schema := tempSubgraphSchema(contractName, namespace, subgraphName, includeTags)
	_, publishError := r.client.PublishSubgraph(ctx, subgraphName, namespace, schema)

	// Publishing can report deployment problems even though composition worked, so check the
	// source graph itself rather than the publish result.
	sourceResponse, apiError = r.client.GetFederatedGraph(ctx, sourceName, namespace)
	if apiError != nil || !sourceResponse.GetGraph().GetIsComposable() {
		reason := "the source graph still did not compose"
		if publishError != nil {
			reason = publishError.Error()
		} else if apiError != nil {
			reason = apiError.Error()
		}
		diags.Append(r.removeTempSubgraph(ctx, temp)...)
		diags.AddError(ErrCreatingContract, fmt.Sprintf(
			"Published temporary subgraph %q, but source graph %q did not compose: %s", subgraphName, sourceName, reason))
		return nil, diags
	}

	return temp, diags
}

// tempSubgraphDeleted reports whether a DeleteSubgraph result means the subgraph is gone. Once the
// temporary subgraph is deleted the source graph has nothing to compose, so Cosmo answers
// ERR_SUBGRAPH_COMPOSITION_FAILED ("At least one subgraph is required for federation."), even though
// the subgraph was deleted. A deployment failure also happens after the delete.
func tempSubgraphDeleted(apiError *api.ApiError) bool {
	return apiError == nil || api.IsSubgraphCompositionFailedError(apiError) || api.IsNotFoundError(apiError) ||
		apiError.Status == common.EnumStatusCode_ERR_DEPLOYMENT_FAILED
}

// removeTempSubgraph deletes the temporary subgraph. It keeps going even if Terraform is being
// interrupted, because a leftover subgraph is worse than a slightly slower stop. Failures are
// warnings: by now the contract may exist, and an error would make Terraform replace it.
func (r *contractResource) removeTempSubgraph(ctx context.Context, temp *tempSubgraph) diag.Diagnostics {
	var diags diag.Diagnostics

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	apiError := r.client.DeleteSubgraph(cleanupCtx, temp.name, temp.namespace)
	if !tempSubgraphDeleted(apiError) {
		diags.AddWarning("Temporary Subgraph Not Deleted", fmt.Sprintf(
			"Could not delete temporary subgraph %q in namespace %q: %s\n\n"+
				"Delete it yourself (in Cosmo Studio, or with: wgc subgraph delete %s -n %s). Until then its dummy "+
				"Query field is part of the source graph's schema. If the contract was created, every plan for it warns "+
				"while the subgraph exists; if not, the next apply reuses the subgraph and deletes it.",
			temp.name, temp.namespace, apiError.Error(), temp.name, temp.namespace))
	}

	return diags
}
