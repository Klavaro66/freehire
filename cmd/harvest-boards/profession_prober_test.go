package main

import (
	"context"
	"testing"
)

func TestProfessionProberDiscoversAndProbesCategorySitemaps(t *testing.T) {
	p := professionProber{}
	getter := fakeGetter{
		professionHarvestSitemapIndex: `<?xml version="1.0"?>
<sitemapindex>
  <sitemap><loc>https://www.profession.hu/sitemap-listings-itdev-hu.xml</loc></sitemap>
  <sitemap><loc>https://www.profession.hu/sitemap-listings-engineering-hu.xml</loc></sitemap>
  <sitemap><loc>https://www.profession.hu/sitemap-listings-itdev-hu.xml</loc></sitemap>
  <sitemap><loc>https://www.profession.hu/sitemap-listings-index-hu.xml</loc></sitemap>
</sitemapindex>`,
		"https://www.profession.hu/sitemap-listings-engineering-hu.xml": `<?xml version="1.0"?>
<urlset>
  <url><loc>https://www.profession.hu/allas/python-fejleszto-acme-2991001</loc></url>
  <url><loc>https://www.profession.hu/allasok/engineering</loc></url>
</urlset>`,
	}

	boards, err := p.discover(context.Background(), getter)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(boards) != 2 || boards[0] != "itdev" || boards[1] != "engineering" {
		t.Fatalf("boards = %v, want [itdev engineering]", boards)
	}

	name, open, err := p.probe(context.Background(), getter, "engineering")
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if name != "" || open != 1 {
		t.Errorf("probe = (%q, %d), want (\"\", 1)", name, open)
	}
}

func TestProfessionBoardFromSitemapRejectsNonCategoryURLs(t *testing.T) {
	for _, raw := range []string{
		"https://www.profession.hu/sitemap-listings-index-hu.xml",
		"https://www.profession.hu/sitemap-companies-hu.xml",
		"not a URL",
	} {
		if got := professionBoardFromSitemap(raw); got != "" {
			t.Errorf("professionBoardFromSitemap(%q) = %q, want empty", raw, got)
		}
	}
}
