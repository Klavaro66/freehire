package atsapply

import (
	"context"
	"testing"

	"github.com/strelov1/freehire/internal/applyform"
	"github.com/strelov1/freehire/internal/autoapply"
)

// Lever always parks on its captcha before any fetcher or browser is touched — a nil
// fetchers map would panic if this short-circuit were ever removed, which is deliberate:
// it proves nothing downstream runs for this provider.
func TestSubmit_LeverAlwaysParksOnCaptchaWithoutTouchingFetchersOrBrowser(t *testing.T) {
	c := &Client{fetchers: nil}

	result, err := c.Submit(context.Background(), autoapply.Claimed{Provider: "lever"}, nil)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Status != autoapply.StatusParked || result.Reason != "requires_captcha" {
		t.Errorf("result = %+v, want parked/requires_captcha", result)
	}
}

func TestMergedFromAPIOnly_SkipsHiddenAndInfoFields(t *testing.T) {
	api := applyform.Form{Fields: []applyform.Field{
		{ID: "keep", Type: applyform.TypeText, Required: true},
		{ID: "gh_src", Type: applyform.TypeHidden},
		{ID: "blurb", Type: applyform.TypeInfo},
	}}

	got := mergedFromAPIOnly(api)

	if len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("merged = %+v, want only the one answerable field", got)
	}
}

func TestDomKindFor_MapsEveryFieldType(t *testing.T) {
	cases := map[applyform.FieldType]string{
		applyform.TypeText:        "text",
		applyform.TypeTextarea:    "textarea",
		applyform.TypeSelect:      "select",
		applyform.TypeMultiSelect: "select",
		applyform.TypeFile:        "file",
		applyform.TypeBoolean:     "checkbox_group",
		applyform.TypeDate:        "text",
		applyform.TypeNumber:      "text",
	}
	for ft, want := range cases {
		if got := domKindFor(ft); got != want {
			t.Errorf("domKindFor(%q) = %q, want %q", ft, got, want)
		}
	}
}
