package sources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// professionTestHTTP is a deterministic fake for Profession sitemap discovery
// and detail-page hydration.
type professionTestHTTP struct {
	texts     map[string]string
	html      map[string]string
	textErrs  map[string]error
	htmlErrs  map[string]error
	htmlCalls []string
}

func (f *professionTestHTTP) GetText(
	_ context.Context,
	rawURL string,
) (string, error) {
	if err := f.textErrs[rawURL]; err != nil {
		return "", err
	}

	value, ok := f.texts[rawURL]
	if !ok {
		return "", errors.New("unexpected GetText: " + rawURL)
	}

	return value, nil
}

func (f *professionTestHTTP) GetHTML(
	_ context.Context,
	rawURL string,
) (*xhtml.Node, error) {
	f.htmlCalls = append(f.htmlCalls, rawURL)

	if err := f.htmlErrs[rawURL]; err != nil {
		return nil, err
	}

	value, ok := f.html[rawURL]
	if !ok {
		return nil, errors.New("unexpected GetHTML: " + rawURL)
	}

	return xhtml.Parse(strings.NewReader(value))
}

func TestProfessionPostingFromURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantID  string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "canonical",
			rawURL:  "https://www.profession.hu/allas/backend-developer-example-kft-budapest-1234567",
			wantID:  "1234567",
			wantURL: "https://www.profession.hu/allas/backend-developer-example-kft-budapest-1234567",
			wantOK:  true,
		},
		{
			name:    "bare host",
			rawURL:  "https://profession.hu/allas/backend-developer-example-kft-1234567",
			wantID:  "1234567",
			wantURL: "https://profession.hu/allas/backend-developer-example-kft-1234567",
			wantOK:  true,
		},
		{
			name:    "query and fragment removed",
			rawURL:  "https://www.profession.hu/allas/backend-developer-example-kft-1234567?foo=bar#details",
			wantID:  "1234567",
			wantURL: "https://www.profession.hu/allas/backend-developer-example-kft-1234567",
			wantOK:  true,
		},
		{
			name:   "wrong host",
			rawURL: "https://example.com/allas/backend-developer-1234567",
			wantOK: false,
		},
		{
			name:   "not job path",
			rawURL: "https://www.profession.hu/cegek/example-1234567",
			wantOK: false,
		},
		{
			name:   "missing id",
			rawURL: "https://www.profession.hu/allas/backend-developer",
			wantOK: false,
		},
		{
			name:   "non numeric id",
			rawURL: "https://www.profession.hu/allas/backend-developer-abc",
			wantOK: false,
		},
		{
			name:   "invalid url",
			rawURL: "not a url",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := professionPostingFromURL(tt.rawURL)

			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}

			if !tt.wantOK {
				return
			}

			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}

			if got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
		})
	}
}

// TestProfessionCrawl verifies sitemap discovery, invalid URL filtering and
// deduplication by Profession's stable posting ID.
func TestProfessionCrawl(t *testing.T) {
	child1 := "https://www.profession.hu/sitemap-listings-1.xml"
	child2 := "https://www.profession.hu/sitemap-listings-2.xml"

	client := &professionTestHTTP{
		texts: map[string]string{
			professionSitemapIndexURL: `
				<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<sitemap><loc>` + child1 + `</loc></sitemap>
					<sitemap><loc>` + child2 + `</loc></sitemap>
				</sitemapindex>`,
			child1: `
				<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<url><loc>https://www.profession.hu/allas/job-one-company-111</loc></url>
					<url><loc>https://www.profession.hu/allas/job-two-company-222</loc></url>
					<url><loc>https://example.com/allas/not-profession-333</loc></url>
				</urlset>`,
			child2: `
				<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<url><loc>https://www.profession.hu/allas/duplicate-job-111</loc></url>
					<url><loc>https://www.profession.hu/allas/job-three-company-333</loc></url>
				</urlset>`,
		},
	}

	source := profession{http: client}

	got, err := source.crawl(context.Background())
	if err != nil {
		t.Fatalf("crawl() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len(crawl()) = %d, want 3", len(got))
	}

	wantIDs := []string{"111", "222", "333"}

	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("posting[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}

// A failed child sitemap must fail discovery instead of returning a silently
// truncated catalogue.
func TestProfessionCrawlFailsOnChildError(t *testing.T) {
	child := "https://www.profession.hu/sitemap-listings-1.xml"

	client := &professionTestHTTP{
		texts: map[string]string{
			professionSitemapIndexURL: `
				<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<sitemap><loc>` + child + `</loc></sitemap>
				</sitemapindex>`,
		},
		textErrs: map[string]error{
			child: errors.New("boom"),
		},
	}

	source := profession{http: client}

	if _, err := source.crawl(context.Background()); err == nil {
		t.Fatal("crawl() error = nil, want error")
	}
}

// FetchNew must avoid detail requests for known postings while fully hydrating
// postings that have not been seen before.
func TestProfessionFetchNewHydratesNewOnly(t *testing.T) {
	child := "https://www.profession.hu/sitemap-listings-1.xml"

	seenURL := "https://www.profession.hu/allas/seen-job-company-111"
	newURL := "https://www.profession.hu/allas/new-job-company-222"

	client := &professionTestHTTP{
		texts: map[string]string{
			professionSitemapIndexURL: `
				<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<sitemap><loc>` + child + `</loc></sitemap>
				</sitemapindex>`,
			child: `
				<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
					<url><loc>` + seenURL + `</loc></url>
					<url><loc>` + newURL + `</loc></url>
				</urlset>`,
		},
		html: map[string]string{
			newURL: professionTestJobPage(
				"Backend Developer",
				"Example Kft.",
			),
		},
	}

	source := profession{http: client}

	jobs, err := source.FetchNew(
		context.Background(),
		CompanyEntry{},
		func(externalID string) bool {
			return externalID == "111"
		},
	)
	if err != nil {
		t.Fatalf("FetchNew() error = %v", err)
	}

	if len(jobs) != 2 {
		t.Fatalf("len(FetchNew()) = %d, want 2", len(jobs))
	}

	byID := make(map[string]Job)
	for _, job := range jobs {
		byID[job.ExternalID] = job
	}

	seen := byID["111"]
	if !seen.SeenRefresh {
		t.Error("seen job SeenRefresh = false, want true")
	}

	fresh := byID["222"]
	if fresh.SeenRefresh {
		t.Error("new job SeenRefresh = true, want false")
	}

	if fresh.Title != "Backend Developer" {
		t.Errorf("new job Title = %q, want %q", fresh.Title, "Backend Developer")
	}

	if len(client.htmlCalls) != 1 || client.htmlCalls[0] != newURL {
		t.Errorf("detail calls = %v, want [%s]", client.htmlCalls, newURL)
	}
}

// TestProfessionDetailMapsJob covers the primary JSON-LD mapping together with
// Profession-specific HTML classification fields.
func TestProfessionDetailMapsJob(t *testing.T) {
	rawURL := "https://www.profession.hu/allas/backend-developer-example-kft-1234567"

	client := &professionTestHTTP{
		html: map[string]string{
			rawURL: `
				<html>
				<head>
					<script type="application/ld+json">
					{
						"@context": "https://schema.org",
						"@type": "JobPosting",
						"title": "Backend &amp; Platform Developer",
						"description": "<p>Build &amp; maintain services.</p>",
						"datePosted": "2026-08-20",
						"employmentType": "Alkalmazotti jogviszony",
						"experienceRequirements": "3-5 év tapasztalat",
						"jobLocation": {
							"@type": "Place",
							"address": {
								"@type": "PostalAddress",
								"addressLocality": "Budapest",
								"addressRegion": "Budapest",
								"addressCountry": "Magyarország"
							}
						},
						"hiringOrganization": {
							"@type": "Organization",
							"name": "Example &amp; Company Kft."
						}
					}
					</script>
				</head>
				<body>
					<div class="address-data">Hibrid munkavégzés</div>
					<div class="classificationType">
						<div class="bullet-wrapper">Alkalmazotti jogviszony</div>
						<div class="bullet-wrapper">Teljes munkaidő</div>
					</div>
				</body>
				</html>`,
		},
	}

	source := profession{http: client}

	got, err := source.detail(
		context.Background(),
		professionPosting{
			ID:  "1234567",
			URL: rawURL,
		},
	)
	if err != nil {
		t.Fatalf("detail() error = %v", err)
	}

	if got.ExternalID != "1234567" {
		t.Errorf("ExternalID = %q, want %q", got.ExternalID, "1234567")
	}

	if got.URL != rawURL {
		t.Errorf("URL = %q, want %q", got.URL, rawURL)
	}

	if got.Title != "Backend & Platform Developer" {
		t.Errorf("Title = %q", got.Title)
	}

	if got.Company != "Example & Company Kft." {
		t.Errorf("Company = %q", got.Company)
	}

	if got.Location != "Budapest" {
		t.Errorf("Location = %q, want Budapest", got.Location)
	}

	if len(got.Countries) != 1 || got.Countries[0] != "hu" {
		t.Errorf("Countries = %v, want [hu]", got.Countries)
	}

	if got.WorkMode != "hybrid" {
		t.Errorf("WorkMode = %q, want hybrid", got.WorkMode)
	}

	if got.Remote {
		t.Error("Remote = true for hybrid job, want false")
	}

	if got.EmploymentType != "full_time" {
		t.Errorf("EmploymentType = %q, want full_time", got.EmploymentType)
	}

	if got.ExperienceYearsMin == nil || *got.ExperienceYearsMin != 3 {
		t.Errorf("ExperienceYearsMin = %v, want 3", got.ExperienceYearsMin)
	}

	wantPostedAt := time.Date(
		2026,
		time.August,
		20,
		0,
		0,
		0,
		0,
		time.UTC,
	)

	if got.PostedAt == nil || !got.PostedAt.Equal(wantPostedAt) {
		t.Errorf("PostedAt = %v, want %v", got.PostedAt, wantPostedAt)
	}
}

// TestProfessionDetailUsesHTMLFallbacks covers fields Profession may omit from
// JobPosting JSON-LD but exposes through stable semantic HTML.
func TestProfessionDetailUsesHTMLFallbacks(t *testing.T) {
	rawURL := "https://www.profession.hu/allas/test-job-company-7654321"

	client := &professionTestHTTP{
		html: map[string]string{
			rawURL: `
				<html>
				<head>
					<script type="application/ld+json">
					{
						"@context": "https://schema.org",
						"@type": "JobPosting",
						"title": "Test Engineer",
						"description": "<p>Test systems.</p>",
						"datePosted": "2026-08-21T10:30:00+02:00",
						"hiringOrganization": {
							"@type": "Organization",
							"name": "Example Kft."
						}
					}
					</script>
				</head>
				<body>
					<div itemprop="jobLocation">
						<span itemprop="addressLocality">
							9000 Magyarország,Győr-Moson-Sopron,Győr
						</span>
					</div>

					<div class="address-data">
						<span itemprop="addressLocality">
							9000 Magyarország,Győr-Moson-Sopron,Győr
						</span>
						Távmunka
					</div>

					<div class="classificationType">
						<div class="bullet-wrapper">Részmunkaidő</div>
					</div>
				</body>
				</html>`,
		},
	}

	source := profession{http: client}

	got, err := source.detail(
		context.Background(),
		professionPosting{
			ID:  "7654321",
			URL: rawURL,
		},
	)
	if err != nil {
		t.Fatalf("detail() error = %v", err)
	}

	if got.Location != "Győr, Győr-Moson-Sopron" {
		t.Errorf(
			"Location = %q, want %q",
			got.Location,
			"Győr, Győr-Moson-Sopron",
		)
	}

	if len(got.Countries) != 1 || got.Countries[0] != "hu" {
		t.Errorf("Countries = %v, want [hu]", got.Countries)
	}

	if got.WorkMode != "remote" {
		t.Errorf("WorkMode = %q, want remote", got.WorkMode)
	}

	if !got.Remote {
		t.Error("Remote = false, want true")
	}

	if got.EmploymentType != "part_time" {
		t.Errorf("EmploymentType = %q, want part_time", got.EmploymentType)
	}
}

func TestProfessionExperienceYearsMin(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  *int
	}{
		{"range", "1-3 év tapasztalat", intPtr(1)},
		{"range en dash", "5–10 év tapasztalat", intPtr(5)},
		{"single", "3 év tapasztalat", intPtr(3)},
		{"plus", "3+ év tapasztalat", intPtr(3)},
		{"none", "Nem kell tapasztalat", intPtr(0)},
		{"without experience", "Tapasztalat nélkül", intPtr(0)},
		{"empty", "", nil},
		{"unknown", "Pályakezdő", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := professionExperienceYearsMin(tt.value)

			if tt.want == nil {
				if got != nil {
					t.Fatalf("got %d, want nil", *got)
				}
				return
			}

			if got == nil {
				t.Fatalf("got nil, want %d", *tt.want)
			}

			if *got != *tt.want {
				t.Errorf("got %d, want %d", *got, *tt.want)
			}
		})
	}
}

func TestProfessionPostedAt(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Time
		wantNil bool
		wantErr bool
	}{
		{
			name:  "date only",
			value: "2026-08-20",
			want: time.Date(
				2026, time.August, 20,
				0, 0, 0, 0,
				time.UTC,
			),
		},
		{
			name:  "rfc3339",
			value: "2026-08-20T12:30:00+02:00",
			want: time.Date(
				2026, time.August, 20,
				10, 30, 0, 0,
				time.UTC,
			),
		},
		{
			name:    "empty",
			value:   "",
			wantNil: true,
		},
		{
			name:    "invalid",
			value:   "20 August 2026",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := professionPostedAt(tt.value)

			if tt.wantErr {
				if err == nil {
					t.Fatal("error = nil, want error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantNil {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("got nil")
			}

			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfessionEmploymentType(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "full time",
			value: "FULL_TIME",
			want:  "full_time",
		},
		{
			name:  "part time",
			value: "PART_TIME",
			want:  "part_time",
		},
		{
			name:  "legal relationship ignored",
			value: "Alkalmazotti jogviszony",
			want:  "",
		},
		{
			name:  "contractor relationship ignored",
			value: "Vállalkozói jogviszony",
			want:  "",
		},
		{
			name: "array",
			value: []any{
				"Alkalmazotti jogviszony",
				"FULL_TIME",
			},
			want: "full_time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := professionEmploymentType(tt.value)

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfessionNormalizeHTMLLocation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "budapest",
			value: "Budapest",
			want:  "Budapest",
		},
		{
			name:  "profession ats location",
			value: "9000 Magyarország,Győr-Moson-Sopron,Győr",
			want:  "Győr, Győr-Moson-Sopron",
		},
		{
			name:  "same city and region",
			value: "Magyarország,Budapest,Budapest",
			want:  "Budapest",
		},
		{
			name:  "whitespace",
			value: "  9000 Magyarország, Győr-Moson-Sopron, Győr  ",
			want:  "Győr, Győr-Moson-Sopron",
		},
		{
			name:  "empty",
			value: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := professionNormalizeHTMLLocation(tt.value)

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// professionTestJobPage returns a minimal valid Profession-style JobPosting
// page for tests that only need successful detail hydration.
func professionTestJobPage(
	title string,
	company string,
) string {
	return `
		<html>
		<head>
			<script type="application/ld+json">
			{
				"@context": "https://schema.org",
				"@type": "JobPosting",
				"title": "` + title + `",
				"description": "<p>Test description.</p>",
				"datePosted": "2026-08-20",
				"hiringOrganization": {
					"@type": "Organization",
					"name": "` + company + `"
				}
			}
			</script>
		</head>
		<body></body>
		</html>`
}
