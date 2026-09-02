package atsapply

import (
	"fmt"
	"strings"

	"github.com/strelov1/freehire/internal/autoapply"
)

// ResolvedField is one field ready to fill, with the exact value the widget expects — an
// option's platform VALUE for a select/checkbox_group, the answer text verbatim otherwise.
type ResolvedField struct {
	ID    string
	Kind  string
	Multi bool
	Value string
}

// Plan is the outcome of resolving every merged field against a candidate's known answers.
type Plan struct {
	Fields   []ResolvedField
	Unmapped []autoapply.UnmappedField
}

// FullyResolved reports whether every required question was answered — the gate Submit
// checks before it ever fills or presses submit on a real form.
func (p Plan) FullyResolved() bool {
	return len(p.Unmapped) == 0
}

// answerKeyFor maps a field's DOM identifier to the key it is looked up under in the
// answers map (internal/candidateprofile.Profile.Fields()). Greenhouse happens to name its
// own standard identity fields (first_name, last_name, email, phone) the same as the answer
// keys already, so most of this map is the identity case; candidate-location is the one
// alias the 2026-09-02 spike measured.
//
// NOT covered here, deliberately: "country" has no answer key at all — the candidate
// profile carries one combined `location` string, not a separate country. On Greenhouse,
// where `country` renders as its own required field on nearly every posting, this means a
// posting requiring it always parks in this package's current scope. Widening the answer
// source (a dedicated country fact, or Tier C/LLM-drafted answers) is future work, not a bug
// here — see design.md's Non-Goals.
var answerKeyFor = map[string]string{
	"first_name":              "first_name",
	"last_name":               "last_name",
	"full_name":               "full_name",
	"email":                   "email",
	"phone":                   "phone",
	"location":                "location",
	"candidate-location":      "location",
	"linkedin":                "linkedin",
	"github":                  "github",
	"portfolio":               "portfolio",
	"authorized_countries":    "authorized_countries",
	"visa_sponsorship_needed": "visa_sponsorship_needed",
	"desired_salary":          "desired_salary",
	"notice_period":           "notice_period",
	"willing_to_relocate":     "willing_to_relocate",
	"age_18_or_older":         "age_18_or_older",
}

// Resolve matches every merged field against the candidate's known answers. A required
// field with no usable answer is reported in Unmapped rather than guessed; an optional one
// with no answer is simply left out of both lists — nothing to fill, nothing wrong either.
func Resolve(fields []MergedField, answers map[string]string) Plan {
	var plan Plan
	for _, f := range fields {
		resolved, reason, ok := resolveOne(f, answers)
		switch {
		case ok:
			plan.Fields = append(plan.Fields, resolved)
		case f.Required:
			plan.Unmapped = append(plan.Unmapped, autoapply.UnmappedField{
				ID: f.ID, Label: f.Label, Required: true, Reason: reason,
			})
		}
		// An optional, unresolved field: neither filled nor reported. Nothing here drafts
		// an answer for it (no Tier C yet), so leaving it blank is a valid outcome.
	}
	return plan
}

func resolveOne(f MergedField, answers map[string]string) (ResolvedField, string, bool) {
	if f.Kind == "file" {
		// File attachment (résumé/cover letter) needs its own artifact-resolution
		// plumbing this package does not build — see the package doc. Always unmapped so
		// a required upload never silently goes out empty.
		return ResolvedField{}, "file uploads are not resolved by this package", false
	}

	key, known := answerKeyFor[f.ID]
	if !known {
		return ResolvedField{}, fmt.Sprintf("no known answer source for %q", f.ID), false
	}
	value, stated := answers[key]
	if !stated || strings.TrimSpace(value) == "" {
		return ResolvedField{}, fmt.Sprintf("candidate has not stated %q", key), false
	}

	if len(f.Options) == 0 {
		return ResolvedField{ID: f.ID, Kind: f.Kind, Multi: f.Multi, Value: value}, "", true
	}

	for _, opt := range f.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Label), strings.TrimSpace(value)) {
			return ResolvedField{ID: f.ID, Kind: f.Kind, Multi: f.Multi, Value: opt.Value}, "", true
		}
	}
	return ResolvedField{}, fmt.Sprintf("answer %q matches none of this field's offered options", value), false
}
