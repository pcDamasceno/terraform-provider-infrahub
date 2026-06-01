package sdkintrospect

import (
	"os"
	"testing"

	"github.com/marcom4rtinez/infrahub-terraform-provider-generator/pkg/queryir"
)

func TestLoadFirewall(t *testing.T) {
	src, err := os.ReadFile("../queryir/testdata/firewall.gql")
	if err != nil {
		t.Fatal(err)
	}
	q, err := queryir.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Load("../../../sdk", q)
	if err != nil {
		t.Fatal(err)
	}
	if res.SDKFunc != "FirewallPolicyRules" {
		t.Errorf("SDKFunc = %q, want FirewallPolicyRules", res.SDKFunc)
	}
	var sa *RNode
	for _, c := range res.Root.Children {
		if c.IR.GqlField == "source_address" {
			sa = c
		}
	}
	if sa == nil || len(sa.Variants) != 2 {
		t.Fatalf("source_address variants unresolved: %#v", sa)
	}
	for _, v := range sa.Variants {
		if v.GoConcrete == "" {
			t.Errorf("variant %s has empty GoConcrete", v.TypeCond)
		}
		if len(v.Children) == 0 {
			t.Errorf("variant %s resolved no fields", v.TypeCond)
		}
	}
}
