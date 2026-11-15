package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Real photographs for screens. Pexels and Unsplash are used when the person
// has put a key in PEXELS_API_KEY or UNSPLASH_ACCESS_KEY; Openverse (openly
// licensed images, no key) is the fallback. Every saved photo records who made
// it and under which licence in assets/credits.json, because a licence that
// asks for credit is a promise the finished design has to keep.

// Variables so tests can point at a local server.
var (
	pexelsAPI    = "https://api.pexels.com/v1/search"
	unsplashAPI  = "https://api.unsplash.com/search/photos"
	openverseAPI = "https://api.openverse.org/v1/images/"
	commonsAPI   = "https://commons.wikimedia.org/w/api.php"
)

// Photo is one search result.
type Photo struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	License  string `json:"license"`
	Page     string `json:"page"`
	URL      string `json:"url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	// Track is Unsplash's download-registration URL.
	Track string `json:"track,omitempty"`
}

// Credit is the line a design shows when the licence asks for one.
func (p Photo) Credit() string {
	switch p.Provider {
	case "unsplash":
		return fmt.Sprintf("Photo by %s on Unsplash", p.Author)
	case "pexels":
		return fmt.Sprintf("Photo by %s on Pexels", p.Author)
	}
	if l := strings.ToLower(p.License); l == "cc0" || l == "pdm" || strings.Contains(l, "public domain") || strings.Contains(l, "cc0") {
		return ""
	}
	return fmt.Sprintf("%q by %s, %s", p.Title, p.Author, p.License)
}

// PhotoProviders says which sources are available right now.
func PhotoProviders() []string {
	var p []string
	if os.Getenv("PEXELS_API_KEY") != "" {
		p = append(p, "pexels")
	}
	if os.Getenv("UNSPLASH_ACCESS_KEY") != "" {
		p = append(p, "unsplash")
	}
	return append(p, "commons", "openverse")
}

// FindPhotos searches the first provider that has results. orientation is
// portrait, landscape or square.
func FindPhotos(ctx context.Context, query, orientation string) ([]Photo, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("photo: say what the photograph shows, in plain words (\"harbour ferry at dawn\")")
	}
	var lastErr error
	for _, prov := range PhotoProviders() {
		var res []Photo
		var err error
		switch prov {
		case "pexels":
			res, err = searchPexels(ctx, query, orientation)
		case "unsplash":
			res, err = searchUnsplash(ctx, query, orientation)
		case "commons":
			res, err = searchCommons(ctx, query, orientation)
		default:
			res, err = searchOpenverse(ctx, query, orientation)
		}
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", prov, err)
			continue
		}
		if len(res) > 0 {
			return res, nil
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("photo search failed (%v). Set PEXELS_API_KEY or UNSPLASH_ACCESS_KEY for reliable results, or use the image action for a generated picture", lastErr)
	}
	return nil, fmt.Errorf("no photograph found for %q; try plainer, more concrete words", query)
}

func getJSONWith(ctx context.Context, u string, headers map[string]string, v any) error {
	raw, err := httpGetWith(ctx, u, headers, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func searchPexels(ctx context.Context, q, orient string) ([]Photo, error) {
	u := pexelsAPI + "?per_page=15&query=" + url.QueryEscape(q)
	if orient != "" {
		u += "&orientation=" + orient
	}
	var doc struct {
		Photos []struct {
			ID           int    `json:"id"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			URL          string `json:"url"`
			Photographer string `json:"photographer"`
			Alt          string `json:"alt"`
			Src          struct {
				Large2x string `json:"large2x"`
			} `json:"src"`
		} `json:"photos"`
	}
	if err := getJSONWith(ctx, u, map[string]string{"Authorization": os.Getenv("PEXELS_API_KEY")}, &doc); err != nil {
		return nil, err
	}
	var out []Photo
	for _, p := range doc.Photos {
		out = append(out, Photo{Provider: "pexels", ID: fmt.Sprint(p.ID), Title: p.Alt, Author: p.Photographer, License: "Pexels licence", Page: p.URL, URL: p.Src.Large2x, Width: p.Width, Height: p.Height})
	}
	return out, nil
}

func searchUnsplash(ctx context.Context, q, orient string) ([]Photo, error) {
	u := unsplashAPI + "?per_page=15&query=" + url.QueryEscape(q)
	switch orient {
	case "portrait":
		u += "&orientation=portrait"
	case "landscape":
		u += "&orientation=landscape"
	case "square":
		u += "&orientation=squarish"
	}
	var doc struct {
		Results []struct {
			ID     string `json:"id"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Alt    string `json:"alt_description"`
			URLs   struct {
				Regular string `json:"regular"`
			} `json:"urls"`
			User  struct{ Name string } `json:"user"`
			Links struct {
				HTML     string `json:"html"`
				Download string `json:"download_location"`
			} `json:"links"`
		} `json:"results"`
	}
	if err := getJSONWith(ctx, u, map[string]string{"Authorization": "Client-ID " + os.Getenv("UNSPLASH_ACCESS_KEY")}, &doc); err != nil {
		return nil, err
	}
	var out []Photo
	for _, p := range doc.Results {
		out = append(out, Photo{Provider: "unsplash", ID: p.ID, Title: p.Alt, Author: p.User.Name, License: "Unsplash licence", Page: p.Links.HTML, URL: p.URLs.Regular, Width: p.Width, Height: p.Height, Track: p.Links.Download})
	}
	return out, nil
}

func searchOpenverse(ctx context.Context, q, orient string) ([]Photo, error) {
	u := openverseAPI + "?page_size=20&category=photograph&license_type=commercial&size=large&q=" + url.QueryEscape(q)
	switch orient {
	case "portrait":
		u += "&aspect_ratio=tall"
	case "landscape":
		u += "&aspect_ratio=wide"
	case "square":
		u += "&aspect_ratio=square"
	}
	var doc struct {
		Results []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Creator string `json:"creator"`
			License string `json:"license"`
			Version string `json:"license_version"`
			URL     string `json:"url"`
			Landing string `json:"foreign_landing_url"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
		} `json:"results"`
	}
	if err := getJSONWith(ctx, u, nil, &doc); err != nil {
		return nil, err
	}
	var out []Photo
	for _, p := range doc.Results {
		// A no-derivatives licence forbids the cropping a screen needs.
		if strings.Contains(strings.ToLower(p.License), "nd") || p.URL == "" {
			continue
		}
		lic := strings.ToUpper(p.License)
		if p.Version != "" && !strings.EqualFold(p.License, "pdm") {
			lic = "CC " + lic + " " + p.Version
		}
		author := p.Creator
		if author == "" {
			author = "unknown author"
		}
		out = append(out, Photo{Provider: "openverse", ID: p.ID, Title: p.Title, Author: author, License: lic, Page: p.Landing, URL: p.URL, Width: p.Width, Height: p.Height})
	}
	return out, nil
}

// metaValue turns a Commons metadata value (a string, but sometimes a number
// or null) into text; null becomes empty.
func metaValue(v any) any {
	if v == nil {
		return ""
	}
	return v
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// searchCommons searches Wikimedia Commons: real photographs of real places,
// every one under a free licence, no key needed.
func searchCommons(ctx context.Context, q, orient string) ([]Photo, error) {
	u := commonsAPI + "?action=query&generator=search&gsrnamespace=6&gsrlimit=24&prop=imageinfo&iiprop=url%7Cextmetadata%7Csize&iiurlwidth=1600&format=json&gsrsearch=" + url.QueryEscape("filetype:bitmap "+q)
	var doc struct {
		Query struct {
			Pages map[string]struct {
				Index     int    `json:"index"`
				Title     string `json:"title"`
				ImageInfo []struct {
					Width    int    `json:"width"`
					Height   int    `json:"height"`
					ThumbURL string `json:"thumburl"`
					URL      string `json:"url"`
					Page     string `json:"descriptionurl"`
					Meta     map[string]struct {
						Value any `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := getJSONWith(ctx, u, nil, &doc); err != nil {
		return nil, err
	}
	type ranked struct {
		idx int
		p   Photo
	}
	var rs []ranked
	for _, pg := range doc.Query.Pages {
		if len(pg.ImageInfo) == 0 {
			continue
		}
		ii := pg.ImageInfo[0]
		src := firstOf(ii.ThumbURL, ii.URL)
		if src == "" || ii.Width < 900 {
			continue
		}
		switch orient {
		case "portrait":
			if float64(ii.Height) < float64(ii.Width)*1.1 {
				continue
			}
		case "landscape":
			if float64(ii.Width) < float64(ii.Height)*1.1 {
				continue
			}
		}
		lic := strings.TrimSpace(fmt.Sprint(metaValue(ii.Meta["LicenseShortName"].Value)))
		if lic == "" || strings.Contains(strings.ToLower(lic), "fair use") {
			continue
		}
		author := strings.TrimSpace(htmlTag.ReplaceAllString(fmt.Sprint(metaValue(ii.Meta["Artist"].Value)), ""))
		if author == "" {
			author = "unknown author"
		}
		title := strings.TrimSuffix(strings.TrimPrefix(pg.Title, "File:"), filepath.Ext(pg.Title))
		rs = append(rs, ranked{pg.Index, Photo{Provider: "commons", ID: pg.Title, Title: title, Author: author, License: lic, Page: ii.Page, URL: src, Width: ii.Width, Height: ii.Height}})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].idx < rs[j].idx })
	var out []Photo
	for _, r := range rs {
		out = append(out, r.p)
	}
	return out, nil
}

// SavePhoto downloads one photo into the project's assets and records its
// credit. It returns the src to use, the pixel size and the bytes.
func SavePhoto(ctx context.Context, workDir, project, name string, p Photo) (src string, w, h int, data []byte, mime string, err error) {
	data, err = httpGetWith(ctx, p.URL, nil, 16<<20)
	if err != nil {
		return "", 0, 0, nil, "", fmt.Errorf("download: %w", err)
	}
	mime = http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return "", 0, 0, nil, "", fmt.Errorf("download: the file is %s, not an image", mime)
	}
	src, w, h, err = SaveAsset(workDir, project, name, data, mime)
	if err != nil {
		return "", 0, 0, nil, "", err
	}
	if p.Track != "" {
		// Unsplash asks API clients to register a download.
		_, _ = httpGetWith(ctx, p.Track, map[string]string{"Authorization": "Client-ID " + os.Getenv("UNSPLASH_ACCESS_KEY")}, 1<<16)
	}
	recordCredit(workDir, project, src, p)
	return src, w, h, data, mime, nil
}

func creditsPath(workDir, project string) string {
	return filepath.Join(AssetsDir(workDir, project), "credits.json")
}

func recordCredit(workDir, project, src string, p Photo) {
	m := map[string]Photo{}
	if raw, err := os.ReadFile(creditsPath(workDir, project)); err == nil {
		_ = json.Unmarshal(raw, &m)
	}
	m[src] = p
	if raw, err := json.MarshalIndent(m, "", "  "); err == nil {
		_ = os.WriteFile(creditsPath(workDir, project), raw, 0o644)
	}
}

// Credits lists the credit lines a project owes for the photos it uses.
func Credits(workDir, project string) []string {
	m := map[string]Photo{}
	raw, err := os.ReadFile(creditsPath(workDir, project))
	if err != nil || json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var out []string
	for src, p := range m {
		if c := p.Credit(); c != "" {
			out = append(out, fmt.Sprintf("%s: %s (%s)", src, c, p.Page))
		}
	}
	sort.Strings(out)
	return out
}
