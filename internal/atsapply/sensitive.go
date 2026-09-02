package atsapply

import "strings"

// sensitiveTerms is a direct port of freehire-apply/internal/drafting's isSensitive list
// (a sibling, more mature paid repo — already measured against real Ashby postings there,
// not invented fresh here). A question matching any of these is never drafted, regardless
// of how confident a draft would be — see Drafter's doc comment for why the check runs
// before the model is ever called.
var sensitiveTerms = []string{
	"salary", "compensation", "sponsor", "visa", "work authoriz", "right to work",
	"gender", "race", "ethnic", "veteran", "disab", "demographic", "sexual orientation",
}

// isSensitiveLabel reports whether a question's label text concerns compensation, work
// authorization/visa sponsorship, or an equal-opportunity/demographic category — the
// categories a candidate's answer must never be guessed or drafted for, only ever taken
// from a fact the candidate stated directly (see labelAnswerKeyFor's visa_sponsorship_needed
// case) or left to park.
func isSensitiveLabel(label string) bool {
	lower := strings.ToLower(label)
	for _, term := range sensitiveTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}
