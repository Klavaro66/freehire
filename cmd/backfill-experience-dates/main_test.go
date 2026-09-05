package main

import (
	"testing"
	"time"

	"github.com/strelov1/freehire/internal/candidate/perioddate"
)

func TestParseOrFallback(t *testing.T) {
	createdAt := time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		raw  string
		want *perioddate.PeriodDate
	}{
		{"parses cleanly", "October 2018", &perioddate.PeriodDate{Year: 2018, Month: 10}},
		{"bare year parses", "2024", &perioddate.PeriodDate{Year: 2024}},
		{"empty is a real absence, no fallback", "", nil},
		{"whitespace-only is a real absence, no fallback", "   ", nil},
		{"Present is a real absence, no fallback", "Present", nil},
		{"present-label case/whitespace insensitive", "  current  ", nil},
		{"garbled text falls back to created_at year", "sometime last year", &perioddate.PeriodDate{Year: 2022}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOrFallback(tc.raw, createdAt)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("parseOrFallback(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
			if got != nil && *got != *tc.want {
				t.Fatalf("parseOrFallback(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestIsFallback(t *testing.T) {
	createdAt := time.Date(2022, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"parsed value is not a fallback", "2024", false},
		{"empty is not a fallback", "", false},
		{"present label is not a fallback", "Present", false},
		{"garbled text is a fallback", "sometime last year", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isFallback(tc.raw, parseOrFallback(tc.raw, createdAt))
			if got != tc.want {
				t.Errorf("isFallback(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
