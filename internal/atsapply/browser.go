package atsapply

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// stealthAllocatorOptions are the launch options the 2026-09-02 spike measured against
// bot.sannysoft.com: headless, plus disable-blink-features=AutomationControlled, which
// alone flipped navigator.webdriver from true to false — matching what Patchright achieved,
// at zero extra dependency. See design.md's "chromedp, not a Python/Patchright sidecar"
// decision for the full comparison and its caveats (untested against real ATS bot-detection,
// datacenter IP reputation unaddressed either way).
func stealthAllocatorOptions() []chromedp.ExecAllocatorOption {
	return append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
}

// pageLoadTimeout bounds how long a single navigation+render may take before this package
// gives up on the attempt. Generous: an application form pulls in its own JS bundle and,
// on Greenhouse, a client-side render pass.
const pageLoadTimeout = 20 * time.Second

// newBrowser starts one browser process for one attempt. Callers must call the returned
// cancel to tear it down — nothing here pools or reuses a browser across attempts, so one
// attempt's session can never leak state (cookies, a half-filled form) into the next.
func (c *Client) newBrowser(ctx context.Context) (context.Context, context.CancelFunc, error) {
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, c.allocatorOpts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	cancel := func() {
		cancelBrowser()
		cancelAlloc()
	}
	// Force the browser to actually start now rather than lazily on the first action, so
	// a launch failure surfaces here instead of inside renderedHTML with a less specific
	// error.
	if err := chromedp.Run(browserCtx); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start browser: %w", err)
	}
	return browserCtx, cancel, nil
}

// renderedHTML navigates to url, waits for readySelector to appear (the application form
// itself — proof the client-side render pass that reveals fields like Greenhouse's
// `country` has actually run), and returns the page's rendered HTML.
func renderedHTML(ctx context.Context, url, readySelector string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pageLoadTimeout)
	defer cancel()

	var pageHTML string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(readySelector, chromedp.ByID),
		chromedp.OuterHTML("html", &pageHTML),
	)
	if err != nil {
		return "", err
	}
	return pageHTML, nil
}

// greenhouseFormReadySelector is Greenhouse's own form element id, confirmed against a
// live posting in the 2026-09-02 spike.
const greenhouseFormReadySelector = "application-form"
