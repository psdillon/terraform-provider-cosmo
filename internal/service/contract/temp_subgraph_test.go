package contract

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/common"
	platformv1 "github.com/wundergraph/cosmo/connect-go/gen/proto/wg/cosmo/platform/v1"
	"github.com/wundergraph/cosmo/terraform-provider-cosmo/internal/api"
)

// Cosmo's graph name rules (controlplane/src/core/util.ts).
var cosmoGraphName = regexp.MustCompile(`^[\dA-Za-z]+(?:[./@_-][\dA-Za-z]+)*$`)

// Cosmo rejects requests whose timeout is over 120000ms, and the Connect client sends the context
// deadline as the request timeout.
func TestCleanupTimeoutWithinCosmoLimit(t *testing.T) {
	if cleanupTimeout >= 2*time.Minute {
		t.Fatalf("cleanupTimeout %s must be under Cosmo's 2 minute request limit", cleanupTimeout)
	}
}

func TestTempSubgraphName(t *testing.T) {
	if got := tempSubgraphName("my-contract"); got != "temp-contract-bootstrap-my-contract" {
		t.Fatalf("unexpected name %q", got)
	}

	long := strings.Repeat("a", 70) + "-" + strings.Repeat("b", 29)
	other := strings.Repeat("a", 70) + "-" + strings.Repeat("c", 29)
	got := tempSubgraphName(long)
	if len(got) > maxGraphNameLength {
		t.Fatalf("name too long: %d", len(got))
	}
	if !strings.HasPrefix(got, tempSubgraphPrefix) || !cosmoGraphName.MatchString(got) {
		t.Fatalf("invalid name %q", got)
	}
	if got == tempSubgraphName(other) {
		t.Fatalf("long names that differ only at the end must not clash")
	}
	if got != tempSubgraphName(long) {
		t.Fatalf("name must be deterministic")
	}

	// A cut that lands on a separator must not leave the name ending in one.
	cut := strings.Repeat("a", maxGraphNameLength-len(tempSubgraphPrefix)-9-1) + "-" + strings.Repeat("z", 20)
	if got := tempSubgraphName(cut); !cosmoGraphName.MatchString(got) {
		t.Fatalf("invalid name after cut %q", got)
	}
}

func TestGraphMatchesLabels(t *testing.T) {
	matchers := parseLabelMatchers([]string{"team=a,team=b", "env=prod"})

	cases := []struct {
		name     string
		matchers [][]string
		labels   []string
		want     bool
	}{
		{"no matchers, no labels", nil, nil, true},
		{"no matchers, labels", nil, []string{"team=a"}, false},
		{"matchers, no labels", matchers, nil, false},
		{"every matcher satisfied", matchers, []string{"team=b", "env=prod"}, true},
		{"one matcher unsatisfied", matchers, []string{"team=a"}, false},
		{"extra labels are fine", matchers, []string{"team=a", "env=prod", "x=y"}, true},
	}
	for _, c := range cases {
		if got := graphMatchesLabels(c.matchers, c.labels); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChooseLabels(t *testing.T) {
	if labels := chooseLabels(nil, nil); labels != nil {
		t.Fatalf("a source graph without matchers needs a subgraph without labels, got %v", labels)
	}

	source := parseLabelMatchers([]string{"team=a,team=b", "env=prod"})

	// team=a would also match "other", so team=b must be chosen.
	others := []otherGraph{{name: "other", matchers: parseLabelMatchers([]string{"team=a"})}}
	labels := chooseLabels(source, others)
	if strings.Join(labels, ",") != "env=prod,team=b" {
		t.Fatalf("unexpected labels %v", labels)
	}
	if !graphMatchesLabels(source, labels) {
		t.Fatalf("chosen labels must match the source graph")
	}

	// A shared option needs only one label.
	shared := parseLabelMatchers([]string{"team=a,env=prod", "env=prod"})
	if labels := chooseLabels(shared, nil); strings.Join(labels, ",") != "env=prod" {
		t.Fatalf("expected a single label, got %v", labels)
	}
}

func TestAffectedGraphs(t *testing.T) {
	source := &platformv1.FederatedGraph{Id: "src", Name: "source", LabelMatchers: []string{"team=a"}}
	graphs := []*platformv1.FederatedGraph{
		source,
		{Id: "plain", Name: "no-matchers"},
		{Id: "same", Name: "same-team", LabelMatchers: []string{"team=a"}},
		{Id: "other", Name: "other-team", LabelMatchers: []string{"team=b"}},
		{Id: "c1", Name: "source-contract", LabelMatchers: []string{"team=a"},
			Contract: &platformv1.Contract{SourceFederatedGraphId: "src"}},
		{Id: "c2", Name: "same-team-contract", Contract: &platformv1.Contract{SourceFederatedGraphId: "same"}},
		{Id: "c3", Name: "other-team-contract", Contract: &platformv1.Contract{SourceFederatedGraphId: "other"}},
	}

	got := strings.Join(affectedGraphs(source, graphs, []string{"team=a"}), "; ")
	want := `contract "same-team-contract"; contract "source-contract"; graph "same-team"`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	// Without labels, only graphs without matchers are affected.
	noMatchers := &platformv1.FederatedGraph{Id: "src", Name: "source"}
	got = strings.Join(affectedGraphs(noMatchers, graphs[1:4], nil), "; ")
	if got != `graph "no-matchers"` {
		t.Fatalf("got %s", got)
	}

	if affected := affectedGraphs(source, []*platformv1.FederatedGraph{source}, []string{"team=a"}); len(affected) != 0 {
		t.Fatalf("the source graph alone must not count as affected, got %v", affected)
	}
}

func TestTempSubgraphReadme(t *testing.T) {
	// Cosmo Cloud's firewall rejects "; delete" with a bare 403.
	if readme := tempSubgraphReadme("c", "ns", "s"); strings.Contains(strings.ToLower(readme), "; delete") {
		t.Fatalf("readme contains a phrase Cosmo Cloud blocks: %s", readme)
	}
}

func TestTempSubgraphSchema(t *testing.T) {
	schema := tempSubgraphSchema("my-contract", "default", "temp-contract-bootstrap-my-contract", []string{"public", "partner"})
	for _, want := range []string{
		"TEMPORARY SUBGRAPH - SAFE TO DELETE",
		`cosmo_contract "my-contract"`,
		`It is safe to delete subgraph "temp-contract-bootstrap-my-contract" in namespace "default"`,
		`_tempContractBootstrap: String @shareable @tag(name: "public") @tag(name: "partner")`,
	} {
		if !strings.Contains(schema, want) {
			t.Errorf("schema is missing %q:\n%s", want, schema)
		}
	}

	if schema := tempSubgraphSchema("c", "ns", "s", nil); !strings.Contains(schema, "_tempContractBootstrap: String @shareable\n") {
		t.Errorf("unexpected field without include tags:\n%s", schema)
	}
}

func TestTempSubgraphDeleted(t *testing.T) {
	// What Cosmo Cloud returned when the last subgraph of a source graph with a contract was deleted.
	compositionFailed := api.NewApiErrorWithErr(common.EnumStatusCode_ERR_SUBGRAPH_COMPOSITION_FAILED,
		"At least one subgraph is required for federation.", api.ErrSubgraphCompositionFailed)

	cases := []struct {
		name string
		err  *api.ApiError
		want bool
	}{
		{"ok", nil, true},
		{"last subgraph removed", compositionFailed, true},
		{"already gone", api.NewApiErrorWithErr(common.EnumStatusCode_ERR_NOT_FOUND, "", api.ErrNotFound), true},
		{"deployment failed", api.NewApiErrorWithErr(common.EnumStatusCode_ERR_DEPLOYMENT_FAILED, "", api.ErrUnknown), true},
		{"other error", api.NewApiErrorWithErr(common.EnumStatusCode_ERR, "boom", api.ErrGeneral), false},
	}
	for _, c := range cases {
		if got := tempSubgraphDeleted(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
