package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	professionHarvestBaseURL      = "https://www.profession.hu"
	professionHarvestSitemapIndex = professionHarvestBaseURL + "/sitemap-listings-index-hu.xml"
)

type professionProber struct{}

type professionSitemapIndex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

type professionSitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// discover reads Profession.hu's authoritative category sitemap index.
// Each sitemap-listings-<board>-hu.xml entry becomes one board candidate,
// so newly introduced categories are picked up without a curated allowlist.
func (professionProber) discover(ctx context.Context, c httpClient) ([]string, error) {
	raw, err := c.GetText(ctx, professionHarvestSitemapIndex)
	if err != nil {
		return nil, fmt.Errorf("profession discover: %w", err)
	}
	var index professionSitemapIndex
	if err := xml.Unmarshal([]byte(raw), &index); err != nil {
		return nil, fmt.Errorf("profession discover: decode sitemap index: %w", err)
	}

	boards := make([]string, 0, len(index.Sitemaps))
	seen := make(map[string]bool, len(index.Sitemaps))

	for _, sm := range index.Sitemaps {
		board := professionBoardFromSitemap(sm.Loc)
		if board == "" || seen[board] {
			continue
		}
		seen[board] = true
		boards = append(boards, board)
	}

	if len(boards) == 0 {
		return nil, fmt.Errorf("profession discover: sitemap index contained no category boards")
	}

	return boards, nil
}

// probe checks that a discovered category sitemap still resolves and contains postings.
// The employer name is deliberately empty: Profession is an aggregator and each posting
// supplies its own employer; harvest-boards will use the board id as the catalog label.
func (professionProber) probe(ctx context.Context, c httpClient, board string) (string, int, error) {
	if strings.TrimSpace(board) == "" {
		return "", 0, nil
	}

	u := fmt.Sprintf("%s/sitemap-listings-%s-hu.xml",
		professionHarvestBaseURL,
		strings.ToLower(strings.TrimSpace(board)),
	)

	raw, err := c.GetText(ctx, u)
	if err != nil {
		return "", 0, err
	}
	var set professionSitemapURLSet
	if err := xml.Unmarshal([]byte(raw), &set); err != nil {
		return "", 0, fmt.Errorf("profession probe %s: decode sitemap: %w", board, err)
	}

	open := 0
	for _, item := range set.URLs {
		if professionHarvestPostingURL(item.Loc) {
			open++
		}
	}

	return "", open, nil
}

func professionBoardFromSitemap(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	name := path.Base(u.Path)
	const (
		prefix = "sitemap-listings-"
		suffix = "-hu.xml"
	)

	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return ""
	}

	board := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if board == "" || board == "index" {
		return ""
	}

	return board
}

func professionHarvestPostingURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	return strings.HasPrefix(u.Path, "/allas/") &&
		strings.Contains(path.Base(u.Path), "-")
}
