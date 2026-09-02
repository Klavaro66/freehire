// Package jobfacts derives a job's employment type, education level, and minimum
// required experience deterministically from its title and description text. Like
// internal/dict/classify and internal/dict/location it is a curated matcher, not a model:
// it resolves explicit signals and emits nothing ("" / nil) for what it cannot
// resolve — it never guesses. Canonical enum values are members of the controlled
// vocabularies the enrichment contract defines (vocab.EmploymentTypeValues /
// vocab.EducationLevelValues).
package jobfacts

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/strelov1/freehire/internal/candidate/hardconstraint/credentials"
)

// reDegreeOptional matches a posting that offers a degree with an
// equivalent-experience alternative, so the hard-constraint evaluator can skip a
// false education blocker. It covers "or equivalent experience" (optionally with a
// qualifier like "work"/"practical") and the bare "degree or equivalent".
var reDegreeOptional = regexp.MustCompile(`(?i)(equivalent(?:\s+\w+){0,2}\s+experience|degree\s+or\s+equivalent)`)

// RequiredCertifications returns the canonical credential slugs the posting
// requires, scanned deterministically from the description with the shared
// credential vocabulary. Computed at read; nothing is stored.
func RequiredCertifications(description string) []string {
	return credentials.Scan(description)
}

// DegreeOptional reports whether the posting offers a degree "or equivalent
// experience", so a candidate without the degree is not falsely blocked.
func DegreeOptional(description string) bool {
	return reDegreeOptional.MatchString(description)
}

// Employment-type matchers, checked in precedence order: a "full-time internship"
// is an internship, a part-time contract is part-time, etc. "temporary" / "fixed
// term" map to contract (the closest vocabulary member). Bare \bintern\b is safe —
// the boundary keeps it out of "internal"/"international". The contract matcher also
// covers the unambiguous US-market shorthands for an independent contractor: 1099
// (the tax form) and "corp-to-corp". "consultant" is deliberately excluded — it is as
// often a full-time title as a contract arrangement.
//
// b2b/c2c also denote an independent-contractor arrangement in some markets, but the
// bare tokens collide with ubiquitous business-model prose ("B2B SaaS", "C2C
// marketplace") — so, like the bare "ms"/"bs" degree abbreviations below, they favour
// precision: reContractShorthand matches them only when an employment-context word
// sits right after ("C2C candidates", "B2B contract", "C2C only"), never standalone.
var (
	reInternship        = regexp.MustCompile(`\b(internship|intern|co-?op|working student|praktikum|werkstudent)\b`)
	rePartTime          = regexp.MustCompile(`\bpart[\s-]?time\b`)
	reContract          = regexp.MustCompile(`\b(contractor|contract|freelancer|freelance|fixed[\s-]?term|temporary|1099|corp[\s-]?to[\s-]?corp)\b`)
	reContractShorthand = regexp.MustCompile(`\b(b2b|c2c)\b[\s:/.-]*(only|contract|contractor|candidates?|welcome|accepted|basis|engagement|employment|position|arrangement|w2)\b`)
	reFullTime          = regexp.MustCompile(`\b(full[\s-]?time|permanent)\b`)
	reFellowship        = regexp.MustCompile(`\bfellowship\b`)
	// reFellowshipStaffRole excludes a "fellowship" mention that names the staff job
	// running the program rather than the fellow's own position — e.g. "Fellowship
	// Manager", "Fellowship Program Coordinator". Those are ordinary full-time/contract
	// roles, so a bare \bfellowship\b would otherwise mislabel them (confirmed live: this
	// pattern dominates real postings mentioning "fellowship", 2026-08-15). The window is
	// bounded to the same line/sentence so a role word elsewhere in a long description
	// (e.g. "reports to the Program Manager") doesn't suppress a genuine fellowship.
	reFellowshipStaffRole = regexp.MustCompile(`fellowship[^\n]{0,50}\b(manager|coordinator|director|officer|administrator)\b|\b(manager|coordinator|director|officer|administrator)\b[^\n]{0,50}fellowship`)
)

// EmploymentType resolves the work arrangement from the title and description,
// returning one of vocab.EmploymentTypeValues or "" when nothing is stated. It
// never assumes full-time for an unstated posting.
func EmploymentType(title, description string) string {
	s := strings.ToLower(title + "\n" + description)
	switch {
	case reFellowship.MatchString(s) && !reFellowshipStaffRole.MatchString(s):
		return "fellowship"
	case reInternship.MatchString(s):
		return "internship"
	case rePartTime.MatchString(s):
		return "part_time"
	case reContract.MatchString(s) || reContractShorthand.MatchString(s):
		return "contract"
	case reFullTime.MatchString(s):
		return "full_time"
	}
	return ""
}

// Requirement facts must not be derived from optional/preferred qualifications.
//
// Job descriptions often contain both hard requirements and a later "preferred"
// section. education_level and english_level are consumed as hard constraints, so
// treating a preferred qualification as required can incorrectly hide a job from
// an otherwise eligible candidate.
//
// Block tags are converted to line boundaries first because scraped job boards
// commonly use <p>/<li> headings rather than punctuation. Explicit optional
// sections are discarded entirely, while inline optional clauses such as
// "Bachelor required, Master's preferred" keep the required clause.
var (
	reFactBlockTag = regexp.MustCompile(`(?i)</?(?:p|br|li|ul|ol|div|h[1-6])\b[^>]*>`)
	reFactHTMLTag  = regexp.MustCompile(`<[^>]+>`)

	reOptionalFactSection = regexp.MustCompile(
		`(?m)^\s*(?:` +
			`(?:az állás betöltéséhez\s+)?előnyt jelent(?:het)?|` +
			`előny(?:ök)?|` +
			`preferred qualifications?|` +
			`nice to have` +
			`)\s*:?\s*$`,
	)

	reOptionalFactMarker = regexp.MustCompile(
		`(?i)\bpreferred\b|\ba plus\b|\bnice to have\b|` +
			`(?:^|[^\p{L}])előny(?:t)?(?:\s+jelent)?(?:$|[^\p{L}])`,
	)

	reFactClause = regexp.MustCompile(`[^;\n.]+`)
)

// requiredFactText removes explicitly optional qualification text before hard
// requirement facts are derived.
//
// The parser intentionally prefers false negatives over false positives here:
// returning no education/English level is safer than turning a preferred
// qualification into a hard candidate constraint.
func requiredFactText(description string) string {
	s := strings.ToLower(description)

	// Preserve structural boundaries from scraped HTML before removing tags.
	s = reFactBlockTag.ReplaceAllString(s, "\n")
	s = reFactHTMLTag.ReplaceAllString(s, " ")

	// Normalize dotted degree abbreviations before sentence-level clause splitting.
	// Otherwise "Ph.D." would be split into unrelated fragments.
	s = strings.ReplaceAll(s, "ph.d.", "phd")
	s = strings.ReplaceAll(s, "ph.d", "phd")
	s = strings.ReplaceAll(s, "m.sc.", "msc")
	s = strings.ReplaceAll(s, "m.sc", "msc")
	s = strings.ReplaceAll(s, "min.", "min")

	// A dedicated preferred section marks everything after it as optional for the
	// purposes of hard requirement derivation. On supported boards these sections
	// follow the mandatory requirements.
	if loc := reOptionalFactSection.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}

	// Remove inline optional clauses without discarding required clauses next to
	// them, e.g. "Bachelor required, Master's preferred".
	clauses := reFactClause.FindAllString(s, -1)
	required := make([]string, 0, len(clauses))

	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}

		// Optional markers qualify the segment immediately preceding them.
		//
		// If the sentence contains a comma, keep only the earlier mandatory segment:
		// "Bachelor's required, PhD preferred" -> "Bachelor's required".
		//
		// Without such a separator the whole clause is optional:
		// "Master's degree is preferred" -> discarded.
		// "MSc végzettség előnyt jelent" -> discarded.
		if loc := reOptionalFactMarker.FindStringIndex(clause); loc != nil {
			prefix := strings.TrimSpace(clause[:loc[0]])

			if comma := strings.LastIndex(prefix, ","); comma >= 0 {
				clause = strings.TrimSpace(strings.TrimRight(prefix[:comma], ",:- "))
			} else {
				continue
			}

			if clause == "" {
				continue
			}
		}

		required = append(required, clause)
	}

	return strings.Join(required, "\n")
}

// Education-level matchers, highest degree first so "Master's or PhD" resolves to
// the ceiling actually named. "none" is emitted only on an explicit negation, and
// only when no positive degree is named (see EducationLevel).
// These favour precision over recall (it is a faceted field — a wrong value is worse
// than a missing one): only unambiguous degree forms match. Bare single-letter
// abbreviations are deliberately excluded — "ms"/"m.s" collide with "MS Office"/
// "MS SQL" and "bs"/"b.s" with everyday text — and bare "master" is excluded because
// "scrum master" is not a degree. The "'s" possessive, an explicit "<level> degree",
// or the -Sc/MBA/PhD tokens are required instead.
// Hungarian forms are included alongside the existing English vocabulary.
// Generic "felsőfokú végzettség" maps conservatively to bachelor: it establishes
// tertiary education, but does not prove that a master's degree is required.
var (
	rePhD      = regexp.MustCompile(`\b(ph\.?\s?d|phd|doctorate|doctoral|doktori|doktori fokozat)\b`)
	reMaster   = regexp.MustCompile(`\b(master'?s|master degree|m\.?sc|mba|graduate degree|mesterképzés|mesterfokozat|msc)\b|(?:^|[^\p{L}\p{N}])mesterfokú(?:$|[^\p{L}\p{N}])`)
	reBachelor = regexp.MustCompile(`\b(bachelor'?s|bachelor degree|b\.?sc|undergraduate degree|alapképzés|alapfokozat|bsc|főiskolai végzettség|egyetemi végzettség|felsőfokú végzettség)\b`)
	reNoDegree = regexp.MustCompile(`\b(no (?:degree|diploma)|degree not required|without a degree|no degree required|végzettség nem szükséges|végzettség nem elvárás)\b`)
)

// EducationLevel resolves the required education from the description, returning
// one of vocab.EducationLevelValues or "" when nothing is stated. A named degree
// wins over a "no degree" phrase (a posting that says "Bachelor's or equivalent;
// no degree required for exceptional candidates" still has a degree signal).
func EducationLevel(description string) string {
	s := requiredFactText(description)
	switch {
	case rePhD.MatchString(s):
		return "phd"
	case reMaster.MatchString(s):
		return "master"
	case reBachelor.MatchString(s):
		return "bachelor"
	case reNoDegree.MatchString(s):
		return "none"
	}
	return ""
}

// experienceCap bounds a parsed years value; anything larger is hyperbole or a
// mis-parse (a stray age/date), not a real experience requirement.
const experienceCap = 50

// ageNoise strips "years of age" / "years old" so an age requirement is not read
// as an experience requirement.
var ageNoise = regexp.MustCompile(`\d{1,2}\s*years?\s*(?:of age|old)`)

// reRangeYears captures the low end of an "N-M years" range; rePlainYears captures
// "N years" / "N+ years" / "N yrs". Both require the number to sit next to a
// year word, so unrelated digits are ignored.
var (
	reRangeYears = regexp.MustCompile(`\b(\d{1,2})\s*(?:-|–|to)\s*\d{1,2}\s*(?:years?|yrs?)`)
	rePlainYears = regexp.MustCompile(`\b(\d{1,2})\s*\+?\s*(?:years?|yrs?)`)
)

// reNoExperience matches an explicit statement that the ROLE needs no prior
// experience. The requirement word must follow the noun with at most a copula
// between, because the same words scoped to an object say the opposite thing about
// the job: "no prior experience WITH Kubernetes is required" makes the tool
// optional while the role may still want a decade.
var reNoExperience = regexp.MustCompile(`\bno\s+(?:prior\s+|previous\s+)?experience\s+(?:is\s+)?(?:required|necessary|needed)\b`)

// reScopedToObject rejects a match whose object TRAILS the requirement word
// instead of leading it — "no prior experience is required WITH our CRM" scopes
// the statement exactly as "…with our CRM is required" does, and guarding only one
// word order let the other through. Go's regexp is RE2 and has no lookahead, so
// the tail is tested separately rather than folded into the pattern above.
//
// A trailing "for" is deliberately absent: "no experience necessary FOR this
// position" is the ordinary entry-level phrasing, where the object is the role
// itself rather than a tool.
var reScopedToObject = regexp.MustCompile(`^\s*(?:with|in|using)\b`)

// statesNoExperience reports whether the description carries an unscoped
// no-experience statement. A description may carry both — a scoped mention and a
// plain one — so every match is examined and one clean statement is enough.
func statesNoExperience(s string) bool {
	for _, loc := range reNoExperience.FindAllStringIndex(s, -1) {
		if !reScopedToObject.MatchString(s[loc[1]:]) {
			return true
		}
	}
	return false
}

// ExperienceYearsMin extracts the minimum required years of experience from the
// description, or nil when none is stated. It takes the smallest year figure
// mentioned next to a year word (the conservative floor) and ignores age phrases
// and out-of-range numbers. An explicit "no prior experience required" resolves
// to 0 — the entry-level population states its requirement in prose rather than
// as a figure, and reading digits alone left it indistinguishable from silence.
func ExperienceYearsMin(description string) *int {
	s := ageNoise.ReplaceAllString(strings.ToLower(description), " ")
	best := -1
	// Zero is the smallest value the walk below can reach, so seeding it also settles
	// precedence: an explicit statement outranks any figure mentioned elsewhere, which
	// is the conservative floor already in force between competing figures.
	if statesNoExperience(s) {
		best = 0
	}
	consider := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil || n < 0 || n > experienceCap {
				continue
			}
			if best == -1 || n < best {
				best = n
			}
		}
	}
	consider(reRangeYears)
	consider(rePlainYears)
	if best == -1 {
		return nil
	}
	return &best
}

// English-level detection. Precision-first like the matchers above: it resolves an
// explicit CEFR code or a well-known level phrase (EN + RU + PL + HU — the Telegram
// sources are Russian-heavy, and Polish boards like NoFluffJobs/JustJoinIT state the
// requirement in Polish, e.g. "Angielski na poziomie min. B2+") and emits "" when
// nothing is stated. Every signal must sit near an English keyword, so a bare
// "B2"/"advanced"/"native" is not misread out of context ("B2B SaaS", "advanced
// degree", "native macOS app"). Values are members of vocab.EnglishLevelValues.
var (
	// reEnglishKw gates the whole parse and anchors every phrase: english_level is
	// about English, so a description that never names it yields nothing.
	// Hungarian job postings commonly use "angol" rather than "English".
	reEnglishKw = regexp.MustCompile(`english|английск|angielsk|angol`)

	// A CEFR code counts only adjacent (either order) to an English keyword.
	// Forward matchers use a non-greedy gap so "English (C1) and German (B2)"
	// resolves the first level associated with English instead of borrowing the
	// later German level.
	reCEFRForward = regexp.MustCompile(`(?:english|английск\w*)[^.\n]{0,60}?\b([abc][12])\b`)
	reCEFRBack    = regexp.MustCompile(`\b([abc][12])\b[^.\n]{0,60}(?:english|английск\w*)`)
	// Polish CEFR proximity tolerates one interior "." that the EN/RU gap above
	// disallows: Polish postings near-universally phrase the requirement as "na
	// poziomie min. B2" — the abbreviation dot in "min." would otherwise break the
	// EN/RU-style no-period gap and hide an otherwise unambiguous, adjacent CEFR code.
	reCEFRForwardPl = regexp.MustCompile(`angielsk\w*[^.\n]{0,20}?\.?[^.\n]{0,10}?\b([abc][12])\b`)
	reCEFRBackPl    = regexp.MustCompile(`\b([abc][12])\b[^.\n]{0,10}\.?[^.\n]{0,20}angielsk\w*`)
	reCEFRForwardHu = regexp.MustCompile(`angol[^.\n]{0,60}?\b([abc][12])\b`)
	reCEFRBackHu    = regexp.MustCompile(`\b([abc][12])\b[^.\n]{0,60}angol`)

	// Hungarian explicit proficiency levels are kept separate from qualitative
	// wording. An explicit level is a stronger requirement signal than wording
	// such as "társalgási", "folyékony" or "tárgyalóképes".
	reHuAdvancedForward     = regexp.MustCompile(`angol[^.\n]{0,60}felsőfok\p{L}*`)
	reHuAdvancedBack        = regexp.MustCompile(`felsőfok\p{L}*[^.\n]{0,60}angol`)
	reHuIntermediateForward = regexp.MustCompile(`angol[^.\n]{0,60}közép(?:fok|szint)\p{L}*`)
	reHuIntermediateBack    = regexp.MustCompile(`közép(?:fok|szint)\p{L}*[^.\n]{0,60}angol`)
	reHuBasicForward        = regexp.MustCompile(`angol[^.\n]{0,60}alap(?:fok|szint)\p{L}*`)
	reHuBasicBack           = regexp.MustCompile(`alap(?:fok|szint)\p{L}*[^.\n]{0,60}angol`)

	// Hungarian qualitative wording is used only when no explicit CEFR or
	// alap-/közép-/felsőfok signal is available.
	reHuFluentForward  = regexp.MustCompile(`angol[^.\n]{0,60}(?:folyékony\p{L}*|tárgyalóképes\p{L}*)`)
	reHuFluentBack     = regexp.MustCompile(`(?:folyékony\p{L}*|tárgyalóképes\p{L}*)[^.\n]{0,60}angol`)
	reHuConversForward = regexp.MustCompile(`angol[^.\n]{0,60}társalgási\p{L}*`)
	reHuConversBack    = regexp.MustCompile(`társalgási\p{L}*[^.\n]{0,60}angol`)

	// Level phrases (checked for English proximity via near). The intermediate family
	// carries its prefix so "upper-intermediate"→b2 and "pre-intermediate"→a2 resolve
	// without a lookbehind (RE2 has none); the Russian "средн" and Polish "średni"
	// families mirror it via their own "above this level" prefix.
	reNative     = regexp.MustCompile(`\bnative\b|родн\w*|носител\w*|anyanyelvi`)
	reFluentAdv  = regexp.MustCompile(`fluen\w*|\badvanced\b|свободн\w*|продвинут\w*|biegł\w*|zaawansowan\w*`)
	reInterFam   = regexp.MustCompile(`\b(upper[\s-]?|pre[\s-]?)?intermediate\b`)
	reRuMidFam   = regexp.MustCompile(`(выше\s+)?средн\w*`)
	rePlMidFam   = regexp.MustCompile(`(wyższy\s+)?średni\w*`)
	reConvers    = regexp.MustCompile(`\bconversational\b|разговорн\w*|komunikatywn\w*|kommunikációs szint\p{L}*`)
	reElementary = regexp.MustCompile(`\belementary\b|\bbeginner\b|начальн\w*|początkując\w*`)
	reBasic      = regexp.MustCompile(`\bbasic\b|базов\w*|podstawow\w*`)
	reNoEnglish  = regexp.MustCompile(`no english|english (?:is )?not required|without english|без английск\w*|bez angielsk\w*|angol (?:nyelvtudás )?nem (?:szükséges|elvárás)`)
)

// englishRank orders the vocabulary lowest→highest so the minimum named level is
// returned — the conservative floor, matching "minimum English level required".
var englishRank = map[string]int{"a1": 1, "a2": 2, "b1": 3, "b2": 4, "c1": 5, "c2": 6, "native": 7}

// englishWindow is the byte gap allowed between an English keyword and a level word
// for the two to count as one signal. Sized for Russian (2 bytes/rune), so ~15 runes.
const englishWindow = 30

// englishScanMaxRunes bounds how much of the description EnglishLevel's near/spanNear
// matching runs over. near/spanNear are O(#keyword-matches × #phrase-matches) in the
// worst case (no pair of matches ever falls within englishWindow of each other), so an
// unbounded, repetitive input — a scraped/SEO-padded description repeating
// "english"/CEFR-like tokens many times without ever pairing them closely — could turn
// derivation of one job into a multi-second, CPU-bound stall on the otherwise fast,
// per-job ingest path. Sized generously above any real job posting; this only ever
// engages on pathological input.
const englishScanMaxRunes = 20000

// EnglishLevel resolves the required English level from the description, returning
// one of vocab.EnglishLevelValues or "" when nothing is stated. When several levels
// are named it returns the lowest (the minimum requirement); an explicit "no English"
// phrase resolves to "none" only when no positive level is present.
// Explicit CEFR codes and explicit Hungarian proficiency levels take precedence
// over qualitative wording. This prevents phrases such as
// "középfokú, társalgási szintű angol" from being downgraded from B2 to B1.
func EnglishLevel(description string) string {
	s := requiredFactText(description)
	s = truncateRunes(s, englishScanMaxRunes)
	if !reEnglishKw.MatchString(s) {
		return ""
	}

	// Scanned once and reused by every near()/spanNear() call below instead of each one
	// re-running FindAllStringIndex over the whole string — near was doing that on every
	// one of its five call sites, and the per-match loops did it once per match.
	kws := reEnglishKw.FindAllStringIndex(s, -1)

	levels := map[string]bool{}

	// Prefer CEFR codes appearing after the English keyword. This prevents a
	// preceding level belonging to another language from being borrowed by English,
	// e.g. "German ... C1. English ... B1".
	forwardCEFR := false

	for _, m := range reCEFRForward.FindAllStringSubmatch(s, -1) {
		levels[m[1]] = true
		forwardCEFR = true
	}
	for _, m := range reCEFRForwardPl.FindAllStringSubmatch(s, -1) {
		levels[m[1]] = true
		forwardCEFR = true
	}
	for _, m := range reCEFRForwardHu.FindAllStringSubmatch(s, -1) {
		levels[m[1]] = true
		forwardCEFR = true
	}

	// Backward forms such as "C1 English" remain supported, but only when no
	// forward CEFR signal was found.
	if !forwardCEFR {
		for _, m := range reCEFRBack.FindAllStringSubmatch(s, -1) {
			levels[m[1]] = true
		}
		for _, m := range reCEFRBackPl.FindAllStringSubmatch(s, -1) {
			levels[m[1]] = true
		}
		for _, m := range reCEFRBackHu.FindAllStringSubmatch(s, -1) {
			levels[m[1]] = true
		}
	}

	// CEFR is the strongest available signal. If an explicit CEFR level is tied to
	// English, qualitative wording elsewhere must not alter it.
	if len(levels) > 0 {
		best := ""
		for lv := range levels {
			if best == "" || englishRank[lv] < englishRank[best] {
				best = lv
			}
		}
		return best
	}

	// Hungarian named proficiency levels are the next strongest signal. They take
	// precedence over qualitative wording such as "társalgási", "folyékony" and
	// "tárgyalóképes".
	if reHuAdvancedForward.MatchString(s) || reHuAdvancedBack.MatchString(s) {
		levels["c1"] = true
	}
	if reHuIntermediateForward.MatchString(s) || reHuIntermediateBack.MatchString(s) {
		levels["b2"] = true
	}
	if reHuBasicForward.MatchString(s) || reHuBasicBack.MatchString(s) {
		levels["a2"] = true
	}

	if len(levels) > 0 {
		best := ""
		for lv := range levels {
			if best == "" || englishRank[lv] < englishRank[best] {
				best = lv
			}
		}
		return best
	}

	// Hungarian qualitative wording is considered only when no explicit CEFR or
	// named Hungarian proficiency level was found.
	if reHuFluentForward.MatchString(s) || reHuFluentBack.MatchString(s) {
		levels["c1"] = true
	}
	if reHuConversForward.MatchString(s) || reHuConversBack.MatchString(s) {
		levels["b1"] = true
	}

	// Existing generic EN/RU/PL qualitative signals.
	if near(s, kws, reNative) {
		levels["native"] = true
	}
	if near(s, kws, reFluentAdv) {
		levels["c1"] = true
	}
	if near(s, kws, reConvers) {
		levels["b1"] = true
	}
	if near(s, kws, reElementary) {
		levels["a1"] = true
	}
	if near(s, kws, reBasic) {
		levels["a2"] = true
	}
	for _, m := range reInterFam.FindAllStringSubmatchIndex(s, -1) {
		if !spanNear(s, kws, m[0], m[1]) {
			continue
		}
		switch {
		case m[2] < 0: // no prefix group — plain "intermediate"
			levels["b1"] = true
		case strings.HasPrefix(s[m[2]:m[3]], "upper"):
			levels["b2"] = true
		case strings.HasPrefix(s[m[2]:m[3]], "pre"):
			levels["a2"] = true
		default:
			levels["b1"] = true
		}
	}
	for _, m := range reRuMidFam.FindAllStringSubmatchIndex(s, -1) {
		if !spanNear(s, kws, m[0], m[1]) {
			continue
		}
		if m[2] >= 0 { // "выше средн..." — above intermediate
			levels["b2"] = true
		} else {
			levels["b1"] = true
		}
	}
	for _, m := range rePlMidFam.FindAllStringSubmatchIndex(s, -1) {
		if !spanNear(s, kws, m[0], m[1]) {
			continue
		}
		if m[2] >= 0 { // "wyższy średni..." — upper-intermediate
			levels["b2"] = true
		} else {
			levels["b1"] = true
		}
	}

	if len(levels) == 0 {
		if reNoEnglish.MatchString(s) {
			return "none"
		}
		return ""
	}
	best := ""
	for lv := range levels {
		if best == "" || englishRank[lv] < englishRank[best] {
			best = lv
		}
	}
	return best
}

// truncateRunes clamps s to at most max runes, rune-boundary safe. Local rather than
// shared: this package stays free of any dependency beyond its own dict-only matching.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// near reports whether any match of phrase in s lies within englishWindow bytes of one
// of kws (English keyword match spans, precomputed once by the caller) without a
// sentence boundary between them.
func near(s string, kws [][]int, phrase *regexp.Regexp) bool {
	for _, p := range phrase.FindAllStringIndex(s, -1) {
		if spanNear(s, kws, p[0], p[1]) {
			return true
		}
	}
	return false
}

// spanNear reports whether [start,end) is within englishWindow bytes of any span,
// with no sentence boundary (. or newline) in the gap — so a level word and an
// English keyword in different sentences ("native iOS apps. English docs") don't
// bind. An overlap always counts.
func spanNear(s string, spans [][]int, start, end int) bool {
	for _, m := range spans {
		var lo, hi int
		switch {
		case start >= m[1]:
			lo, hi = m[1], start
		case m[0] >= end:
			lo, hi = end, m[0]
		default:
			return true // overlap
		}
		if hi-lo <= englishWindow && !strings.ContainsAny(s[lo:hi], ".\n") {
			return true
		}
	}
	return false
}
