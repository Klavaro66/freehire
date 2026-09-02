package atsapply

import "testing"

// A trimmed stand-in for the real Greenhouse application form scanned in the 2026-09-02
// spike (webflow job 7951430) — enough shapes to exercise the parser: plain required/
// optional text inputs, a react-select-style text input with no `required` HTML attribute
// (country), a file upload, a textarea, and a checkbox group sharing one `name` (the EEOC
// shape) that must collapse into one logical field rather than N.
const greenhouseFixtureHTML = `
<html><body>
<form id="application-form">
  <input id="first_name" name="first_name" type="text" required>
  <input id="last_name" name="last_name" type="text" required>
  <input id="email" name="email" type="text" required>
  <input id="country" name="country" type="text">
  <input id="resume" name="resume" type="file" required>
  <textarea id="question_67131484" name="question_67131484"></textarea>
  <input id="gh_src" name="gh_src" type="hidden" value="opaque-token">
  <input id="gender_1" name="gender" type="checkbox" value="Male">
  <input id="gender_2" name="gender" type="checkbox" value="Female">
</form>
</body></html>
`

func TestScanGreenhouseForm_ExtractsPlainFields(t *testing.T) {
	fields, err := ScanGreenhouseForm(greenhouseFixtureHTML)
	if err != nil {
		t.Fatalf("ScanGreenhouseForm: %v", err)
	}

	byID := map[string]DOMField{}
	for _, f := range fields {
		byID[f.ID] = f
	}

	if f, ok := byID["first_name"]; !ok || !f.Required || f.Kind != "text" {
		t.Errorf("first_name = %+v (ok=%v), want required text", f, ok)
	}
	if f, ok := byID["country"]; !ok || f.Required {
		t.Errorf("country = %+v (ok=%v), want present and NOT required (no HTML attribute)", f, ok)
	}
	if f, ok := byID["resume"]; !ok || f.Kind != "file" || !f.Required {
		t.Errorf("resume = %+v (ok=%v), want required file", f, ok)
	}
	if f, ok := byID["question_67131484"]; !ok || f.Kind != "textarea" {
		t.Errorf("question_67131484 = %+v (ok=%v), want textarea", f, ok)
	}
}

func TestScanGreenhouseForm_SkipsHiddenFields(t *testing.T) {
	fields, err := ScanGreenhouseForm(greenhouseFixtureHTML)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.ID == "gh_src" {
			t.Errorf("hidden field gh_src was scanned as %+v, want it skipped — it is platform-filled, never a candidate answer", f)
		}
	}
}

func TestScanGreenhouseForm_GroupsACheckboxGroupByName(t *testing.T) {
	fields, err := ScanGreenhouseForm(greenhouseFixtureHTML)
	if err != nil {
		t.Fatal(err)
	}

	var genderFields []DOMField
	for _, f := range fields {
		if f.Name == "gender" {
			genderFields = append(genderFields, f)
		}
	}
	if len(genderFields) != 1 {
		t.Fatalf("gender fields = %d, want exactly 1 (a checkbox group is one logical field, not one per option)", len(genderFields))
	}
	if !genderFields[0].Multi || genderFields[0].Kind != "checkbox_group" {
		t.Errorf("gender field = %+v, want Multi=true Kind=checkbox_group", genderFields[0])
	}
	if len(genderFields[0].Options) != 2 {
		t.Errorf("gender options = %d, want 2", len(genderFields[0].Options))
	}
}

func TestScanGreenhouseForm_NoApplicationFormIsAnError(t *testing.T) {
	if _, err := ScanGreenhouseForm(`<html><body>not a form page</body></html>`); err == nil {
		t.Fatal("want an error when #application-form is not on the page")
	}
}
