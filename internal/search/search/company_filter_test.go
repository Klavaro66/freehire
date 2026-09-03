package search

import (
	"net/url"
	"testing"
)

// groups coerces the CompanyFilterFromValues result to [][]string (nil when no
// filter), so tests can assert on the AND-of-ORs structure.
func groups(t *testing.T, f any) [][]string {
	t.Helper()
	if f == nil {
		return nil
	}
	g, ok := f.([][]string)
	if !ok {
		t.Fatalf("filter is %T, want [][]string", f)
	}
	return g
}

// hasGroup reports whether gs contains a group exactly equal to want (order of
// fragments within the group matters; order of groups does not).
func hasGroup(gs [][]string, want ...string) bool {
	for _, g := range gs {
		if len(g) != len(want) {
			continue
		}
		eq := true
		for i := range g {
			if g[i] != want[i] {
				eq = false
				break
			}
		}
		if eq {
			return true
		}
	}
	return false
}

func TestCompanyFilterFromValues_NoFacetsIsNil(t *testing.T) {
	if f := CompanyFilterFromValues(url.Values{}); f != nil {
		t.Errorf("empty values should yield nil filter, got %v", f)
	}
	// A bare empty param emits no fragment.
	if f := CompanyFilterFromValues(url.Values{"regions": {""}}); f != nil {
		t.Errorf("empty regions value should yield nil filter, got %v", f)
	}
}

func TestCompanyFilterFromValues_ArrayOverlapOrWithinAndAcross(t *testing.T) {
	// Multiple values within one facet OR; different facets AND.
	f := CompanyFilterFromValues(url.Values{
		"regions":      {"europe", "asia"},
		"company_type": {"startup"},
	})
	gs := groups(t, f)
	if len(gs) != 2 {
		t.Fatalf("want 2 AND groups, got %d: %v", len(gs), gs)
	}
	if !hasGroup(gs, `regions = "europe"`, `regions = "asia"`) {
		t.Errorf("missing ORed regions group in %v", gs)
	}
	// company_type param maps to the plural company_types attribute.
	if !hasGroup(gs, `company_types = "startup"`) {
		t.Errorf("missing company_types group in %v", gs)
	}
}

func TestCompanyFilterFromValues_ParamToAttributeMapping(t *testing.T) {
	f := CompanyFilterFromValues(url.Values{
		"company_size":   {"11-50"},
		"subindustries":  {"Payments"},
		"remote_regions": {"eu"},
	})
	gs := groups(t, f)
	// company_size -> company_sizes, subindustries -> subindustry (scalar), remote_regions passthrough.
	if !hasGroup(gs, `company_sizes = "11-50"`) {
		t.Errorf("company_size should map to company_sizes attribute in %v", gs)
	}
	if !hasGroup(gs, `subindustry = "Payments"`) {
		t.Errorf("subindustries should map to scalar subindustry attribute in %v", gs)
	}
	if !hasGroup(gs, `remote_regions = "eu"`) {
		t.Errorf("missing remote_regions group in %v", gs)
	}
}

func TestCompanyFilterFromValues_ScalarMaturityMembership(t *testing.T) {
	// The scalar maturity facet ORs its values in one group; a NULL company (empty
	// maturity in the document) matches none since the equality is against the value.
	f := CompanyFilterFromValues(url.Values{"maturity": {"startup", "scaleup"}})
	gs := groups(t, f)
	if !hasGroup(gs, `maturity = "startup"`, `maturity = "scaleup"`) {
		t.Errorf("maturity values should OR within one group in %v", gs)
	}
}

func TestCompanyFilterFromValues_YCFacets(t *testing.T) {
	f := CompanyFilterFromValues(url.Values{"yc_stage": {"Growth"}, "yc_flags": {"top_company"}})
	gs := groups(t, f)
	if len(gs) != 2 {
		t.Fatalf("want 2 AND groups, got %d: %v", len(gs), gs)
	}
	if !hasGroup(gs, `yc_stage = "Growth"`) || !hasGroup(gs, `yc_flags = "top_company"`) {
		t.Errorf("YC facets should each AND as their own group in %v", gs)
	}
}
