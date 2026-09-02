package sources

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	stdhtml "html"
	"log"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	xhtml "golang.org/x/net/html"
)

// profession adapts profession.hu, Hungary's largest general-purpose job board.
//
// Profession is boardless and multi-company. Discovery uses the public listing
// sitemap, while posting details are hydrated from each canonical job page.
//
// Profession does not expose every field consistently in JobPosting JSON-LD.
// In particular, some postings omit jobLocation while still exposing the
// location in semantic HTML. Structured JSON-LD remains the preferred source;
// stable semantic page markup is used as a fallback where necessary.
type profession struct {
	http professionHTTP
}

type professionHTTP interface {
	TextGetter
	HTMLGetter
}

const (
	professionSitemapIndexURL = "https://www.profession.hu/sitemap-listings-index-hu.xml"

	// Keep progress visible during the large Profession catalogue without
	// flooding logs with one line per posting.
	professionProgressEvery = 500
)

var (
	professionExperienceRangeRE = regexp.MustCompile(
		`(?i)(\d+)\s*[-–—]\s*\d+\s*év`,
	)
	professionExperienceSingleRE = regexp.MustCompile(
		`(?i)(\d+)\s*\+?\s*év`,
	)
	professionPostalPrefixRE = regexp.MustCompile(
		`^\s*\d{4}\s+`,
	)
)

// NewProfession builds the Profession.hu adapter over the shared HTTP client.
func NewProfession(c professionHTTP) Source {
	return profession{http: c}
}

func (profession) Provider() string {
	return "profession"
}

func (profession) boardless() {}

func (profession) aggregator() {}

// professionSitemapIndex is the standard sitemap-index document.
type professionSitemapIndex struct {
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// professionURLSet is one child sitemap containing canonical posting URLs.
type professionURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// professionPosting is the cheap discovery representation. The numeric id at
// the end of Profession's canonical URL is stable enough to use as ExternalID.
type professionPosting struct {
	ID  string
	URL string
}

// professionJobPosting contains only JobPosting fields Profession exposes in a
// useful structured form.
//
// jobLocation and applicantLocationRequirements are RawMessage deliberately:
// schema.org permits both object and array forms, and Profession uses optional
// fields inconsistently between postings. RawMessage lets us support either
// shape without failing the entire JobPosting decode.
type professionJobPosting struct {
	Title                  string          `json:"title"`
	Description            string          `json:"description"`
	DatePosted             string          `json:"datePosted"`
	EmploymentType         any             `json:"employmentType"`
	ExperienceRequirements string          `json:"experienceRequirements"`
	JobLocationType        string          `json:"jobLocationType"`
	JobLocation            json.RawMessage `json:"jobLocation"`

	ApplicantLocationRequirements json.RawMessage `json:"applicantLocationRequirements"`

	HiringOrganization struct {
		Name string `json:"name"`
	} `json:"hiringOrganization"`
}

type professionPlace struct {
	Address struct {
		AddressLocality string `json:"addressLocality"`
		AddressRegion   string `json:"addressRegion"`
		AddressCountry  any    `json:"addressCountry"`
	} `json:"address"`
}

type professionApplicantLocation struct {
	Name string `json:"name"`
}

// professionProgress tracks detail hydration while fetchDetails executes its
// bounded worker pool.
type professionProgress struct {
	total     int64
	processed atomic.Int64
	cheap     atomic.Int64
	hydrated  atomic.Int64
	failed    atomic.Int64
}

func newProfessionProgress(total int) *professionProgress {
	return &professionProgress{
		total: int64(total),
	}
}

func (p *professionProgress) mark(kind string) {
	switch kind {
	case "cheap":
		p.cheap.Add(1)
	case "hydrated":
		p.hydrated.Add(1)
	case "failed":
		p.failed.Add(1)
	}

	processed := p.processed.Add(1)

	if processed%professionProgressEvery != 0 && processed != p.total {
		return
	}

	var percent int64
	if p.total > 0 {
		percent = processed * 100 / p.total
	}

	log.Printf(
		"profession: progress %d/%d (%d%%) cheap=%d hydrated=%d failed=%d",
		processed,
		p.total,
		percent,
		p.cheap.Load(),
		p.hydrated.Load(),
		p.failed.Load(),
	)
}

// Fetch performs a full hydration crawl. Normal ingestion should generally use
// FetchNew through the HydratingSource interface.
func (s profession) Fetch(
	ctx context.Context,
	_ CompanyEntry,
) ([]Job, error) {
	postings, err := s.crawl(ctx)
	if err != nil {
		return nil, err
	}

	log.Printf(
		"profession: hydration starting: postings=%d workers=%d",
		len(postings),
		defaultDetailWorkers,
	)

	progress := newProfessionProgress(len(postings))

	jobs := fetchDetails(
		postings,
		defaultDetailWorkers,
		func(p professionPosting) (Job, bool) {
			if ctx.Err() != nil {
				progress.mark("failed")
				return Job{}, false
			}

			job, err := s.detail(ctx, p)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf(
						"profession: detail failed: id=%s url=%q error=%v",
						p.ID,
						p.URL,
						err,
					)
				}

				progress.mark("failed")
				return Job{}, false
			}

			progress.mark("hydrated")
			return job, true
		},
	)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return jobs, nil
}

// FetchNew skips expensive detail requests for postings already stored in the
// catalogue. Seen postings are emitted only as SeenRefresh identities.
func (s profession) FetchNew(
	ctx context.Context,
	_ CompanyEntry,
	seen func(externalID string) bool,
) ([]Job, error) {
	postings, err := s.crawl(ctx)
	if err != nil {
		return nil, err
	}

	log.Printf(
		"profession: hydration starting: postings=%d workers=%d",
		len(postings),
		defaultDetailWorkers,
	)

	progress := newProfessionProgress(len(postings))

	jobs := fetchDetails(
		postings,
		defaultDetailWorkers,
		func(p professionPosting) (Job, bool) {
			if ctx.Err() != nil {
				progress.mark("failed")
				return Job{}, false
			}

			if seen(p.ID) {
				progress.mark("cheap")

				return Job{
					ExternalID:  p.ID,
					URL:         p.URL,
					SeenRefresh: true,
				}, true
			}

			job, err := s.detail(ctx, p)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf(
						"profession: detail failed: id=%s url=%q error=%v; deferring to next crawl",
						p.ID,
						p.URL,
						err,
					)
				}

				progress.mark("failed")
				return Job{}, false
			}

			progress.mark("hydrated")
			return job, true
		},
	)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return jobs, nil
}

// crawl walks Profession's listing sitemap index and returns one discovery
// record per unique posting id.
//
// A child-sitemap failure aborts the crawl rather than returning a truncated
// catalogue and pretending it was complete.
func (s profession) crawl(
	ctx context.Context,
) ([]professionPosting, error) {
	raw, err := s.http.GetText(ctx, professionSitemapIndexURL)
	if err != nil {
		return nil, fmt.Errorf(
			"profession: sitemap index GET %q: %w",
			professionSitemapIndexURL,
			err,
		)
	}

	var index professionSitemapIndex
	if err := xml.Unmarshal([]byte(raw), &index); err != nil {
		return nil, fmt.Errorf(
			"profession: sitemap index decode: %w",
			err,
		)
	}

	log.Printf(
		"profession: discovery: %d child sitemaps",
		len(index.Sitemaps),
	)

	var out []professionPosting

	seenIDs := make(map[string]struct{})

	for i, sm := range index.Sitemaps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sitemapURL := strings.TrimSpace(sm.Loc)
		if sitemapURL == "" {
			continue
		}

		raw, err := s.http.GetText(ctx, sitemapURL)
		if err != nil {
			return nil, fmt.Errorf(
				"profession: child sitemap GET failed: sitemap=%d/%d url=%q: %w",
				i+1,
				len(index.Sitemaps),
				sitemapURL,
				err,
			)
		}

		var set professionURLSet
		if err := xml.Unmarshal([]byte(raw), &set); err != nil {
			return nil, fmt.Errorf(
				"profession: child sitemap decode failed: sitemap=%d/%d url=%q: %w",
				i+1,
				len(index.Sitemaps),
				sitemapURL,
				err,
			)
		}

		for _, entry := range set.URLs {
			posting, ok := professionPostingFromURL(entry.Loc)
			if !ok {
				continue
			}

			if _, duplicate := seenIDs[posting.ID]; duplicate {
				continue
			}

			seenIDs[posting.ID] = struct{}{}
			out = append(out, posting)
		}

		if (i+1)%10 == 0 || i+1 == len(index.Sitemaps) {
			log.Printf(
				"profession: discovery progress: sitemaps=%d/%d postings=%d",
				i+1,
				len(index.Sitemaps),
				len(out),
			)
		}
	}

	log.Printf(
		"profession: discovery complete: postings=%d",
		len(out),
	)

	return out, nil
}

// professionPostingFromURL extracts the stable numeric posting id from a
// canonical Profession job URL.
func professionPostingFromURL(
	rawURL string,
) (professionPosting, bool) {
	rawURL = strings.TrimSpace(rawURL)

	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return professionPosting{}, false
	}

	if !strings.EqualFold(u.Hostname(), "profession.hu") &&
		!strings.EqualFold(u.Hostname(), "www.profession.hu") {
		return professionPosting{}, false
	}

	if !strings.Contains(u.Path, "/allas/") {
		return professionPosting{}, false
	}

	base := path.Base(strings.Trim(u.Path, "/"))

	i := strings.LastIndexByte(base, '-')
	if i < 0 || i == len(base)-1 {
		return professionPosting{}, false
	}

	id := base[i+1:]

	for _, r := range id {
		if r < '0' || r > '9' {
			return professionPosting{}, false
		}
	}

	u.RawQuery = ""
	u.Fragment = ""

	return professionPosting{
		ID:  id,
		URL: u.String(),
	}, true
}

// detail maps one Profession detail page.
//
// JSON-LD is preferred for fields it states directly. Profession's stable
// semantic HTML fills fields which JobPosting sometimes omits, notably
// location, work mode and full-time/part-time classification.
func (s profession) detail(
	ctx context.Context,
	p professionPosting,
) (Job, error) {
	root, err := s.http.GetHTML(ctx, p.URL)
	if err != nil {
		return Job{}, fmt.Errorf(
			"HTTP GET: %w",
			err,
		)
	}

	var data professionJobPosting
	if !ldJobPosting(root, &data) {
		return Job{}, fmt.Errorf(
			"schema.org JobPosting not found or could not be decoded",
		)
	}

	title := strings.TrimSpace(data.Title)
	if title == "" {
		return Job{}, fmt.Errorf(
			"JobPosting.title is empty",
		)
	}

	company := strings.TrimSpace(data.HiringOrganization.Name)
	if company == "" {
		return Job{}, fmt.Errorf(
			"JobPosting.hiringOrganization.name is empty",
		)
	}

	location := professionStructuredLocation(data.JobLocation)
	if location == "" {
		location = professionHTMLLocation(root)
	}

	countries := professionCountries(
		data.JobLocation,
		data.ApplicantLocationRequirements,
	)

	if len(countries) == 0 && professionHTMLIsHungary(root) {
		countries = []string{"hu"}
	}

	workMode := professionHTMLWorkMode(root)

	employmentType := professionHTMLEmploymentType(root)
	if employmentType == "" {
		employmentType = professionEmploymentType(
			data.EmploymentType,
		)
	}

	postedAt, err := professionPostedAt(data.DatePosted)
	if err != nil {
		log.Printf(
			"profession: invalid datePosted: id=%s value=%q error=%v",
			p.ID,
			data.DatePosted,
			err,
		)
	}

	return Job{
		ExternalID: p.ID,
		URL:        p.URL,

		Title:   stdhtml.UnescapeString(title),
		Company: stdhtml.UnescapeString(company),

		Location:  location,
		Countries: countries,

		Description: sanitizeHTML(
			stdhtml.UnescapeString(data.Description),
		),

		Remote:   workMode == "remote",
		WorkMode: workMode,

		EmploymentType: employmentType,

		ExperienceYearsMin: professionExperienceYearsMin(
			data.ExperienceRequirements,
		),

		PostedAt: postedAt,
	}, nil
}

// professionStructuredPlaces decodes schema.org jobLocation in either its
// single-object or array form.
func professionStructuredPlaces(
	raw json.RawMessage,
) []professionPlace {
	if len(raw) == 0 ||
		string(raw) == "null" {
		return nil
	}

	var many []professionPlace
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}

	var one professionPlace
	if err := json.Unmarshal(raw, &one); err == nil {
		return []professionPlace{one}
	}

	return nil
}

// professionStructuredLocation builds Location from every structured
// JobPosting.jobLocation place Profession publishes.
func professionStructuredLocation(
	raw json.RawMessage,
) string {
	places := professionStructuredPlaces(raw)

	var locations []string
	seen := make(map[string]struct{})

	for _, place := range places {
		value := professionPlaceLocation(place)
		if value == "" {
			continue
		}

		key := strings.ToLower(value)
		if _, duplicate := seen[key]; duplicate {
			continue
		}

		seen[key] = struct{}{}
		locations = append(locations, value)
	}

	return strings.Join(locations, ", ")
}

func professionPlaceLocation(
	place professionPlace,
) string {
	city := strings.TrimSpace(
		place.Address.AddressLocality,
	)
	region := strings.TrimSpace(
		place.Address.AddressRegion,
	)

	if city != "" &&
		region != "" &&
		strings.EqualFold(city, region) {
		return city
	}

	return joinNonEmpty(
		city,
		region,
	)
}

// professionCountries uses structured geography only:
//
//  1. jobLocation.address.addressCountry
//  2. applicantLocationRequirements
//
// This follows Job.Countries' structured-only contract rather than deriving a
// country from ordinary free-text location.
func professionCountries(
	jobLocation json.RawMessage,
	applicantLocations json.RawMessage,
) []string {
	var out []string
	seen := make(map[string]struct{})

	add := func(country string) {
		if country == "" {
			return
		}

		if _, duplicate := seen[country]; duplicate {
			return
		}

		seen[country] = struct{}{}
		out = append(out, country)
	}

	for _, place := range professionStructuredPlaces(jobLocation) {
		add(
			professionAddressCountry(
				place.Address.AddressCountry,
			),
		)
	}

	for _, requirement := range professionApplicantLocations(
		applicantLocations,
	) {
		add(
			professionCountry(
				requirement.Name,
			),
		)
	}

	return out
}

func professionApplicantLocations(
	raw json.RawMessage,
) []professionApplicantLocation {
	if len(raw) == 0 ||
		string(raw) == "null" {
		return nil
	}

	var many []professionApplicantLocation
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}

	var one professionApplicantLocation
	if err := json.Unmarshal(raw, &one); err == nil {
		return []professionApplicantLocation{one}
	}

	return nil
}

func professionAddressCountry(
	value any,
) string {
	switch v := value.(type) {
	case string:
		return professionCountry(v)

	case map[string]any:
		for _, key := range []string{
			"name",
			"identifier",
			"alternateName",
		} {
			raw, ok := v[key]
			if !ok {
				continue
			}

			s, ok := raw.(string)
			if !ok {
				continue
			}

			if country := professionCountry(s); country != "" {
				return country
			}
		}
	}

	return ""
}

// professionCountry handles Profession's Hungarian country labels and then
// falls back to freehire's shared country normalizer for codes/English names.
func professionCountry(
	value string,
) string {
	value = strings.TrimSpace(value)

	switch strings.ToLower(value) {
	case "magyarország",
		"hu",
		"hun",
		"hungary":
		return "hu"
	}

	countries := countryFromCode(value)
	if len(countries) == 0 {
		return ""
	}

	return countries[0]
}

// professionHTMLEmploymentType reads Profession's explicit classification
// pills. Legal relationship values such as "Alkalmazotti jogviszony" are not
// treated as full-time; only the actual schedule classification is mapped.
func professionHTMLEmploymentType(
	root *xhtml.Node,
) string {
	box := professionNodeByClass(
		root,
		"classificationType",
	)
	if box == nil {
		return ""
	}

	for _, value := range professionTextsByClass(
		box,
		"bullet-wrapper",
	) {
		switch strings.ToLower(
			strings.TrimSpace(value),
		) {
		case "teljes munkaidő":
			return "full_time"

		case "részmunkaidő":
			return "part_time"
		}
	}

	return ""
}

// professionEmploymentType is a conservative JSON-LD fallback. Profession
// commonly uses employmentType for the legal relationship rather than
// full-time/part-time status, so those legal values are deliberately ignored.
func professionEmploymentType(
	value any,
) string {
	var values []string

	switch v := value.(type) {
	case string:
		values = append(values, v)

	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if ok {
				values = append(values, s)
			}
		}

	case []string:
		values = append(values, v...)
	}

	for _, value := range values {
		value = strings.TrimSpace(value)

		switch strings.ToLower(value) {
		case "alkalmazotti jogviszony",
			"vállalkozói jogviszony":
			continue
		}

		if mapped := schemaEmploymentType(value); mapped != "" {
			return mapped
		}
	}

	return ""
}

// professionHTMLWorkMode reads Profession's explicit work-arrangement label.
//
// Do not map JSON-LD jobLocationType=TELECOMMUTE directly to remote:
// Profession uses TELECOMMUTE on hybrid postings as well.
func professionHTMLWorkMode(
	root *xhtml.Node,
) string {
	box := professionNodeByClass(
		root,
		"address-data",
	)
	if box == nil {
		return ""
	}

	value := strings.ToLower(
		professionNodeText(box),
	)

	switch {
	case strings.Contains(value, "hibrid"):
		return "hybrid"

	case strings.Contains(value, "távmunka"):
		return "remote"

	case strings.Contains(value, "remote"):
		return "remote"

	default:
		return ""
	}
}

// professionHTMLLocation is the fallback for postings whose JSON-LD JobPosting
// omits jobLocation.
//
// Scope addressLocality under itemprop=jobLocation first so unrelated location
// markup elsewhere on the page cannot accidentally become the job's location.
func professionHTMLLocation(
	root *xhtml.Node,
) string {
	jobLocation := professionNodeByAttr(
		root,
		"itemprop",
		"jobLocation",
	)

	if jobLocation != nil {
		locality := professionNodeByAttr(
			jobLocation,
			"itemprop",
			"addressLocality",
		)

		if locality != nil {
			return professionNormalizeHTMLLocation(
				professionNodeText(locality),
			)
		}
	}

	// Older/alternate Profession page variants keep the semantic locality in
	// the visible address block without a jobLocation wrapper.
	addressBox := professionNodeByClass(
		root,
		"address-data",
	)

	if addressBox == nil {
		return ""
	}

	locality := professionNodeByAttr(
		addressBox,
		"itemprop",
		"addressLocality",
	)
	if locality == nil {
		return ""
	}

	return professionNormalizeHTMLLocation(
		professionNodeText(locality),
	)
}

// professionNormalizeHTMLLocation normalizes Profession's two observed
// locality shapes:
//
//	Budapest
//	9000 Magyarország,Győr-Moson-Sopron,Győr
//
// The second becomes:
//
//	Győr, Győr-Moson-Sopron
func professionNormalizeHTMLLocation(
	value string,
) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	rawParts := strings.Split(value, ",")

	parts := make([]string, 0, len(rawParts))

	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		part = professionPostalPrefixRE.ReplaceAllString(
			part,
			"",
		)

		if professionCountryLabel(part) {
			continue
		}

		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return ""
	}

	if len(parts) == 1 {
		return parts[0]
	}

	// Profession's ATS-style address locality is ordered
	// country, region, city. After removing country, reverse the final pair
	// into freehire's usual "City, Region" representation.
	city := parts[len(parts)-1]
	region := parts[len(parts)-2]

	if strings.EqualFold(city, region) {
		return city
	}

	return city + ", " + region
}

func professionCountryLabel(
	value string,
) bool {
	switch strings.ToLower(
		strings.TrimSpace(value),
	) {
	case "magyarország",
		"hungary",
		"hu",
		"hun":
		return true

	default:
		return false
	}
}

// professionHTMLIsHungary is a last-resort structured-page fallback for
// country when the semantic location markup explicitly contains Hungary.
func professionHTMLIsHungary(
	root *xhtml.Node,
) bool {
	jobLocation := professionNodeByAttr(
		root,
		"itemprop",
		"jobLocation",
	)

	if jobLocation == nil {
		return false
	}

	locality := professionNodeByAttr(
		jobLocation,
		"itemprop",
		"addressLocality",
	)

	if locality == nil {
		return false
	}

	value := strings.ToLower(
		professionNodeText(locality),
	)

	return strings.Contains(
		value,
		"magyarország",
	) ||
		strings.Contains(
			value,
			"hungary",
		)
}

// professionPostedAt accepts both schema.org's normal RFC3339 timestamp and
// Profession's observed date-only YYYY-MM-DD representation.
func professionPostedAt(
	value string,
) (*time.Time, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return nil, nil
	}

	if parsed, err := time.Parse(
		time.RFC3339,
		value,
	); err == nil {
		parsed = parsed.UTC()
		return &parsed, nil
	}

	parsed, err := time.Parse(
		"2006-01-02",
		value,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unsupported date format: %w",
			err,
		)
	}

	parsed = parsed.UTC()

	return &parsed, nil
}

// professionExperienceYearsMin maps Profession's explicit structured
// experience requirement to the minimum required years.
//
// Examples:
//
//	"1-3 év tapasztalat"  -> 1
//	"5-10 év tapasztalat" -> 5
//	"3 év tapasztalat"    -> 3
//
// Explicit "no experience required" wording maps to zero; no seniority is
// inferred from this number.
func professionExperienceYearsMin(
	value string,
) *int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	lower := strings.ToLower(value)

	for _, phrase := range []string{
		"nem kell tapasztalat",
		"nem igényel tapasztalatot",
		"tapasztalat nélkül",
	} {
		if strings.Contains(lower, phrase) {
			zero := 0
			return &zero
		}
	}

	if match := professionExperienceRangeRE.FindStringSubmatch(
		value,
	); len(match) == 2 {
		years, err := strconv.Atoi(match[1])
		if err == nil {
			return &years
		}
	}

	if match := professionExperienceSingleRE.FindStringSubmatch(
		value,
	); len(match) == 2 {
		years, err := strconv.Atoi(match[1])
		if err == nil {
			return &years
		}
	}

	return nil
}

func professionNodeByClass(
	root *xhtml.Node,
	className string,
) *xhtml.Node {
	if root == nil {
		return nil
	}

	if root.Type == xhtml.ElementNode &&
		professionHasClass(root, className) {
		return root
	}

	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := professionNodeByClass(
			child,
			className,
		); found != nil {
			return found
		}
	}

	return nil
}

func professionNodeByAttr(
	root *xhtml.Node,
	key string,
	value string,
) *xhtml.Node {
	if root == nil {
		return nil
	}

	if root.Type == xhtml.ElementNode {
		for _, attr := range root.Attr {
			if attr.Key == key &&
				strings.EqualFold(
					strings.TrimSpace(attr.Val),
					value,
				) {
				return root
			}
		}
	}

	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := professionNodeByAttr(
			child,
			key,
			value,
		); found != nil {
			return found
		}
	}

	return nil
}

func professionHasClass(
	node *xhtml.Node,
	className string,
) bool {
	for _, attr := range node.Attr {
		if attr.Key != "class" {
			continue
		}

		for _, class := range strings.Fields(attr.Val) {
			if class == className {
				return true
			}
		}
	}

	return false
}

func professionTextsByClass(
	root *xhtml.Node,
	className string,
) []string {
	var out []string

	var walk func(*xhtml.Node)

	walk = func(node *xhtml.Node) {
		if node == nil {
			return
		}

		if node.Type == xhtml.ElementNode &&
			professionHasClass(node, className) {
			if text := professionNodeText(node); text != "" {
				out = append(out, text)
			}

			return
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	return out
}

func professionNodeText(
	root *xhtml.Node,
) string {
	if root == nil {
		return ""
	}

	var builder strings.Builder

	var walk func(*xhtml.Node)

	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			text := strings.TrimSpace(node.Data)

			if text != "" {
				if builder.Len() > 0 {
					builder.WriteByte(' ')
				}

				builder.WriteString(text)
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)

	return strings.Join(
		strings.Fields(builder.String()),
		" ",
	)
}
