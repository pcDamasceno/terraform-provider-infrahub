package emit

import (
	"os"
	"strings"
	"testing"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/sdkintrospect"
)

func TestEmitFirewall(t *testing.T) {
	src, _ := os.ReadFile("../queryir/testdata/firewall.gql")
	q, err := queryir.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	res, err := sdkintrospect.Load("../../../sdk", q)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DataSource(res)
	if err != nil {
		t.Fatalf("emit failed (invalid Go generated): %v", err)
	}
	if !strings.Contains(out, "infrahub_sdk.FirewallPolicyRules(") {
		t.Error("did not call infrahub_sdk.FirewallPolicyRules")
	}
	if !strings.Contains(out, "case *infrahub_sdk.") {
		t.Error("no type-switch over concrete genqlient types")
	}
	if !strings.Contains(out, "ListNestedAttribute") {
		t.Error("no nested list attributes emitted")
	}
	if strings.Contains(out, "Node...") || strings.Contains(out, "node_...") {
		t.Error("emitted the upstream '...' placeholder bug")
	}
}
