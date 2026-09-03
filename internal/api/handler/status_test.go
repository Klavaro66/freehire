package handler

import (
	"testing"
	"time"
)

// statusNow is a fixed reference instant so freshness assertions don't couple to
// the wall clock.
var statusNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

// fresh/stale success timestamps relative to statusNow and the 48h window.
var (
	freshSuccess = statusNow.Add(-1 * time.Hour)  // well within the window
	staleSuccess = statusNow.Add(-72 * time.Hour) // older than 48h
)

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		name string
		roll providerRollup
		want providerStatus
	}{
		{
			name: "all healthy and fresh is operational",
			roll: providerRollup{total: 100, healthy: 100, lastSuccess: freshSuccess},
			want: statusOperational,
		},
		{
			name: "exactly 90 percent healthy and fresh is operational",
			roll: providerRollup{total: 100, healthy: 90, lastSuccess: freshSuccess},
			want: statusOperational,
		},
		{
			name: "a minority failing is degraded",
			roll: providerRollup{total: 100, healthy: 80, lastSuccess: freshSuccess},
			want: statusDegraded,
		},
		{
			name: "almost all failing is down",
			roll: providerRollup{total: 100, healthy: 5, lastSuccess: freshSuccess},
			want: statusDown,
		},
		{
			name: "exactly 10 percent healthy is down",
			roll: providerRollup{total: 100, healthy: 10, lastSuccess: freshSuccess},
			want: statusDown,
		},
		{
			name: "healthy counts but stale success is down",
			roll: providerRollup{total: 100, healthy: 100, lastSuccess: staleSuccess},
			want: statusDown,
		},
		{
			name: "never succeeded is down",
			roll: providerRollup{total: 100, healthy: 100}, // zero lastSuccess = never
			want: statusDown,
		},
		{
			name: "no boards is down (defensive, avoids div-by-zero)",
			roll: providerRollup{total: 0, healthy: 0, lastSuccess: freshSuccess},
			want: statusDown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveStatus(tc.roll, statusNow); got != tc.want {
				t.Errorf("deriveStatus(%+v) = %q, want %q", tc.roll, got, tc.want)
			}
		})
	}
}

func TestFleetStatus(t *testing.T) {
	cases := []struct {
		name  string
		rolls []providerRollup
		want  providerStatus
	}{
		{"empty fleet is operational", nil, statusOperational},
		{
			name:  "all served and fresh is operational",
			rolls: []providerRollup{{total: 100, healthy: 100, lastSuccess: freshSuccess}, {total: 50, healthy: 50, lastSuccess: freshSuccess}},
			want:  statusOperational,
		},
		{
			// The regression this whole change exists for: a single small fully-down
			// provider must NOT red a fleet that is broadly healthy. Worst-provider
			// logic returned down here; the fleet aggregate stays operational.
			name: "a tiny fully-down provider does not red a broadly healthy fleet",
			rolls: []providerRollup{
				{total: 1000, healthy: 1000, lastSuccess: freshSuccess},
				{total: 1, healthy: 0}, // never succeeded, fully cooled
			},
			want: statusOperational,
		},
		{
			name: "a broad outage (most boards cooled) is down",
			rolls: []providerRollup{
				{total: 100, healthy: 5, lastSuccess: freshSuccess},
				{total: 100, healthy: 3, lastSuccess: freshSuccess},
			},
			want: statusDown,
		},
		{
			name: "a large minority cooled is degraded",
			rolls: []providerRollup{
				{total: 100, healthy: 60, lastSuccess: freshSuccess},
				{total: 100, healthy: 90, lastSuccess: freshSuccess},
			},
			want: statusDegraded,
		},
		{
			// Every provider stale (a fleet-wide stall) surfaces as down even at full
			// served fraction — freshness guards a silently stopped crawl.
			name:  "a fleet-wide stall is down despite full served fraction",
			rolls: []providerRollup{{total: 100, healthy: 100, lastSuccess: staleSuccess}},
			want:  statusDown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fleetStatus(tc.rolls, statusNow); got != tc.want {
				t.Errorf("fleetStatus(%v) = %q, want %q", tc.rolls, got, tc.want)
			}
		})
	}
}
