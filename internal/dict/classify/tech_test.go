package classify

import "testing"

func TestIsTech(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  bool
	}{
		// Positives — confident software/IT titles (from the prod unknown bucket).
		{"software engineer", "Senior Software Engineer", true},
		{"software engineer II", "Senior Software Engineer II", true},
		{"web3 developer", "Senior Web3 Developer", true},
		{"salesforce developer", "Senior Salesforce Developer", true},
		{"backend developer", "Backend Developer", true},
		{"full stack developer", "Full Stack Developer", true},
		{"devops engineer", "DevOps Engineer", true},
		{"sre", "Site Reliability Engineer", true},
		{"data scientist", "Lead Data Scientist", true},
		{"ml engineer", "Machine Learning Engineer", true},
		{"system administrator", "Senior System Administrator", true},
		{"it administrator", "Senior IT Administrator für Business Software", true},
		{"python developer", "Python Developer (Remote)", true},
		{"programmer", "COBOL Programmer", true},
		{"qa engineer", "QA Engineer", true},
		// Generalist software titles that state no sub-discipline. MTS is software in
		// 294 of 300 sampled prod postings (xAI, Pure Storage, Cockroach Labs); the
		// semiconductor tail carries its own fab suffixes.
		{"member of technical staff", "Member of Technical Staff", true},
		{"member of the technical staff", "Member of the Technical Staff, Pretraining", true},
		{"founding engineer", "Founding Engineer", true},
		// "AI-native" describes the toolchain, not the discipline — still software.
		{"ai-native engineer", "Senior AI-Native Engineer", true},
		{"ai native engineer", "AI Native Engineer", true},

		// Precision-safe gaps found while auditing technical titles on a
		// general-population job board. Each phrase carries an explicit IT/software
		// anchor; the corresponding bare role nouns remain deliberately absent.
		{"service desk", "French Speaking Service Desk Analyst - Level 1", true},
		{"helpdesk", "Helpdesk munkatárs", true},
		{"soc analyst", "SOC Analyst L2", true},
		{"solution architect", "Presales Solution Architect (Microsoft Cloud)", true},
		{"it project manager", "IT Project Manager", true},
		{"it business analyst", "IT Business Analyst", true},
		{"mes developer", "MES Developer (Delmia Apriso)", true},
		{"java fejlesztő", "Java fejlesztő", true},
		{"java fejlesztők", "JAVA fejlesztők", true},
		{"software mérnök", "Termelési software mérnök", true},
		{"it infrastructure", "IT Infrastructure & Projects Specialist", true},
		{"technical support analyst", "Technical Support Analyst", true},
		{"szoftverfejlesztő", "Szoftverfejlesztő - ERP terület", true},
		{"alkalmazásfejlesztő", "Hardverközeli alkalmazásfejlesztő", true},
		{"python fejlesztő", "Python fejlesztő (Python, Django)", true},
		{"node.js fejlesztő", "Node.js fejlesztő", true},
		{".net fejlesztő", "Senior .net fejlesztő", true},
		{"adatbázis fejlesztő", "Oracle adatbázis fejlesztő", true},
		{"adattárház fejlesztő", "Adattárház fejlesztő", true},
		{"bi fejlesztő", "BI fejlesztő", true},
		{"sap abap fejlesztő", "SAP ABAP fejlesztő (LJK)", true},
		{"pega fejlesztő", "Senior PEGA fejlesztő", true},
		{"odoo fejlesztő", "Odoo-fejlesztő", true},
		{"beágyazott szoftverfejlesztő", "Beágyazott szoftverfejlesztő", true},
		{"mobilalkalmazás-fejlesztő", "Full-stack és mobilalkalmazás-fejlesztő (Flutter/React Native + PHP/Symfony) – Budapest", true},

		// Trap negatives — non-software engineering / non-tech that carry "engineer"
		// or other shared words. These MUST stay unflagged (bias: leave in unknown).
		{"mechanical engineer", "Senior Mechanical Engineer", false},
		{"manufacturing engineer", "Senior Manufacturing Engineer", false},
		{"project engineer", "Sr. Project Engineer", false},
		{"drainage engineer", "Senior Professional Engineer - Drainage", false},
		{"optical engineer", "Senior Optical Characterization Engineer", false},
		{"sales engineer", "Sales Engineer", false},
		{"geologist", "Senior Geologist", false},
		{"business developer", "Business Developer", false},
		// The qualified forms above must not broaden these shared business and
		// infrastructure nouns into technical evidence.
		{"project manager", "Project Manager", false},
		{"business analyst", "Business Analyst", false},
		{"customer support analyst", "Customer Support Analyst", false},
		{"infrastructure project manager", "Infrastructure Project Manager", false},

		// Hungarian false-positive guards: "fejlesztő" appears in many non-software roles.
		{"business development", "Üzletfejlesztő / Sales manager", false},
		{"product development", "Termékfejlesztő mérnök", false},
		{"special education", "Fejlesztőpedagógus", false},
		{"process improvement", "Folyamatfejlesztő LEAN mérnök", false},
		{"organization development", "Szervezetfejlesztési specialista", false},
		{"real estate development", "Ingatlanfejlesztési menedzser", false},
		// "Product Engineer" is deliberately absent from the term list: a prod sample
		// of 300 split 142 software (Attio, clasp, Circleback) against 64 manufacturing
		// (ABB, Howmet Aerospace, Texas Instruments, Flextronics). Not software-anchored,
		// so it stays unknown — the named role carries it instead.
		{"product engineer", "Product Engineer", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTech(tt.title); got != tt.want {
				t.Errorf("IsTech(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

// A software title that spells "design" in the middle must still read as technical.
// "software design engineer" is not adjacent to any techTitleTerms entry — wordmatch
// needs adjacency — so without its own term it fell through to unknown once the
// category stopped being `design`.
func TestIsTech_SoftwareDesignEngineer(t *testing.T) {
	for _, title := range []string{
		"Software Design Engineer",
		"Senior Software Design Engineer",
		"Software Design Engineer in Test",
		// The "-ing" spelling needs its own term: wordmatch is boundary-aware, so
		// "engineer" cannot see "engineering", and these titles carry no category at
		// all — this detector is the only thing left to read them as technical.
		"Software Design Engineering Manager",
		"Director, Software Design Engineering",
	} {
		if !IsTech(title) {
			t.Errorf("IsTech(%q) = false, want true", title)
		}
	}
}
