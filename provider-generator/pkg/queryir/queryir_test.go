package queryir

import (
	"os"
	"testing"
)

func TestParseFirewall(t *testing.T) {
	src, err := os.ReadFile("testdata/firewall.gql")
	if err != nil {
		t.Fatal(err)
	}
	q, err := Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if q.OpName != "FirewallPolicyRules" {
		t.Errorf("OpName = %q, want FirewallPolicyRules", q.OpName)
	}
	if q.RootObject != "SecurityPolicyRule" {
		t.Errorf("RootObject = %q, want SecurityPolicyRule", q.RootObject)
	}
	if q.VarName != "policy" {
		t.Errorf("VarName = %q, want policy", q.VarName)
	}
	if q.Root.Kind != List {
		t.Fatalf("Root.Kind = %v, want List", q.Root.Kind)
	}
	byName := map[string]*Node{}
	for _, c := range q.Root.Children {
		byName[c.GqlField] = c
	}
	if byName["name"] == nil || byName["name"].Kind != Scalar {
		t.Errorf("name should be a Scalar leaf")
	}
	if byName["source_zone"] == nil || byName["source_zone"].Kind != Object {
		t.Errorf("source_zone should be an Object")
	}
	sa := byName["source_address"]
	if sa == nil || sa.Kind != Union {
		t.Fatalf("source_address should be a Union, got %#v", sa)
	}
	conds := map[string]bool{}
	for _, v := range sa.Variants {
		conds[v.TypeCond] = true
	}
	if !conds["SecurityIPAddress"] || !conds["SecurityPrefix"] {
		t.Errorf("source_address variants = %v, want SecurityIPAddress+SecurityPrefix", conds)
	}
}
