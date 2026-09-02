package atsapply

import "testing"

func TestIsSensitiveLabel_CatchesEveryPortedCategory(t *testing.T) {
	labels := []string{
		"What is your desired salary?",
		"What is your current compensation?",
		"Will you now or in the future require sponsorship?",
		"Do you require a visa to work in this country?",
		"Do you have work authorization for this role?",
		"Do you have the right to work in the UK?",
		"What gender do you identify as?",
		"What is your race?",
		"Please specify your ethnic background.",
		"Are you a protected veteran?",
		"Do you have a disability?",
		"This demographic data is voluntary.",
		"What is your sexual orientation?",
	}
	for _, label := range labels {
		if !isSensitiveLabel(label) {
			t.Errorf("isSensitiveLabel(%q) = false, want true", label)
		}
	}
}

func TestIsSensitiveLabel_LeavesOrdinaryQuestionsAlone(t *testing.T) {
	labels := []string{
		"Where did you first hear about this role?",
		"Do you have advanced proficiency in German?",
		"Why do you want to work here?",
		"What is your LinkedIn profile?",
		"Please describe a project you're proud of.",
	}
	for _, label := range labels {
		if isSensitiveLabel(label) {
			t.Errorf("isSensitiveLabel(%q) = true, want false", label)
		}
	}
}

func TestIsSensitiveLabel_IsCaseInsensitive(t *testing.T) {
	if !isSensitiveLabel("WHAT IS YOUR DESIRED SALARY?") {
		t.Error("want the sensitive check to ignore case")
	}
}
