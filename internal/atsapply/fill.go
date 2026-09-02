package atsapply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// fillTimeout bounds one field's interaction. Short: a field either responds immediately or
// it was never a real target on the page.
const fillTimeout = 5 * time.Second

// submitVerifyTimeout bounds how long this waits for a confirmation or refusal marker to
// appear after the submit click.
const submitVerifyTimeout = 15 * time.Second

// CONFIRMATION_MARKERS are positive acknowledgements only, per the reference
// implementation's own rule: matching none of these means "unconfirmed", never "failed".
// Extend, never invert — a false "confirmed" risks recording an application that never went
// through; a false "unconfirmed" only costs a retry.
var confirmationMarkers = []string{
	"thank you for applying",
	"application submitted",
	"application received",
	"we have received your application",
	"we've received your application",
}

// submitRefusedMarkers is text a board renders when it declined the submit click itself —
// an explicit refusal, safe to act on (unlike inverting confirmationMarkers would be).
var submitRefusedMarkers = []string{
	"please try again",
	"there was an error",
}

// fillAndSubmit fills every field the plan resolved, presses submit, and reports whether
// the submission was confirmed. It runs on an already-navigated page (the same session
// renderedHTML used to scan the form) — config always wins here in the sense that matters
// for v1: this package fills strictly from the plan and never reads back or trusts anything
// the page may have pre-filled itself.
//
// This is the least-verified part of the package — see design.md's Testing section and
// task 7.1: correctness here rests on the 2026-09-02 spike's single live posting and the
// reference implementation's own measured rules, not on this package's own live testing.
func fillAndSubmit(ctx context.Context, jobURL string, plan Plan) (bool, error) {
	for _, f := range plan.Fields {
		if err := fillOne(ctx, f); err != nil {
			return false, fmt.Errorf("fill %q: %w", f.ID, err)
		}
	}

	if err := chromedp.Run(ctx, chromedp.Click(greenhouseSubmitSelector, chromedp.ByQuery)); err != nil {
		return false, fmt.Errorf("click submit: %w", err)
	}

	return verifySubmission(ctx)
}

// greenhouseSubmitSelector is Greenhouse's own submit button id.
const greenhouseSubmitSelector = "#submit_app"

func fillOne(parent context.Context, f ResolvedField) error {
	ctx, cancel := context.WithTimeout(parent, fillTimeout)
	defer cancel()

	sel := fieldSelector(f.ID)

	switch f.Kind {
	case "textarea", "text":
		// Text and the react-select-backed autocomplete fields (country,
		// candidate-location) are indistinguishable in this package's DOM scan — both
		// render as a plain <input type="text">. The trailing Enter is a no-op on a
		// plain text field and, for an autocomplete field, commits the first suggestion
		// — the same "type, then confirm" interaction a person uses. Unverified beyond
		// the reference implementation's own account of the pattern; see fill.go's doc.
		return chromedp.Run(ctx,
			chromedp.Clear(sel, chromedp.ByID),
			chromedp.SendKeys(sel, f.Value, chromedp.ByID),
			chromedp.SendKeys(sel, kb.Escape, chromedp.ByID), // close any open suggestion list before Enter, in case the typed text already matched nothing
		)
	case "select":
		return chromedp.Run(ctx, chromedp.SetValue(sel, f.Value, chromedp.ByID))
	case "checkbox_group":
		// f.Value is one option's value; Multi fields may need more than one — see
		// resolve.go's note that this package resolves at most one value per field
		// today (a scope gap alongside file uploads, not handled here).
		optSel := fmt.Sprintf(`input[name=%q][value=%q]`, f.ID, f.Value)
		return chromedp.Run(ctx, chromedp.Click(optSel, chromedp.ByQuery))
	default:
		return fmt.Errorf("no fill strategy for kind %q", f.Kind)
	}
}

// fieldSelector resolves a merged field's id to a DOM selector. IDs from this package's own
// scan are element ids; `#id` is correct for every kind ScanGreenhouseForm produces except
// checkbox_group, which fillOne selects by name+value directly instead.
func fieldSelector(id string) string {
	return "#" + id
}

// verifySubmission waits for either a confirmation or an explicit refusal marker in the
// page text. Neither appearing within the timeout is reported as unconfirmed (false),
// distinct from an error — see the CONFIRMATION_MARKERS doc comment for why the two
// failure directions are not symmetric.
func verifySubmission(parent context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, submitVerifyTimeout)
	defer cancel()

	deadline := time.Now().Add(submitVerifyTimeout)
	for time.Now().Before(deadline) {
		var bodyText string
		if err := chromedp.Run(ctx, chromedp.Text("body", &bodyText, chromedp.ByQuery)); err != nil {
			return false, err
		}
		lower := strings.ToLower(bodyText)
		for _, m := range confirmationMarkers {
			if strings.Contains(lower, m) {
				return true, nil
			}
		}
		for _, m := range submitRefusedMarkers {
			if strings.Contains(lower, m) {
				return false, fmt.Errorf("board refused the submission: matched marker %q", m)
			}
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return false, nil
		}
	}
	return false, nil
}
