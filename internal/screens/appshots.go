package screens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // Store screenshots arrive as PNG or JPEG.
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/design"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Play serves WebP to some clients.
)

// Appshots are the store screenshots of real, shipped apps, put in front of
// the author as one contact sheet before the system is written. The App Store
// side uses Apple's public search API; the Play side reads the public store
// pages and is a fallback, since their markup can change without notice.

// App is one store listing and its screenshots.
type App struct {
	Store     string   `json:"store"`
	Name      string   `json:"name"`
	Developer string   `json:"developer"`
	Genre     string   `json:"genre,omitempty"`
	Rating    float64  `json:"rating,omitempty"`
	Ratings   int      `json:"ratings,omitempty"`
	Page      string   `json:"page"`
	Shots     []string `json:"shots"`
}

// Test hooks.
var (
	itunesAPI = "https://itunes.apple.com/search"
	playBase  = "https://play.google.com"
)

const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

// FindApps searches a store. A query with semicolons names apps one by one
// ("Wise; Monzo; Revolut") and takes the best match of each; a plain query
// takes the top results.
func FindApps(ctx context.Context, query, store string, limit int) ([]App, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("appshots: query names a kind of app (\"tide times\") or apps by name separated by ; (\"Wise; Monzo\")")
	}
	if limit <= 0 {
		limit = 4
	}
	var terms []string
	for _, t := range strings.Split(query, ";") {
		if t = strings.TrimSpace(t); t != "" {
			terms = append(terms, t)
		}
	}
	per := limit
	if len(terms) > 1 {
		per = 1
	}
	search := searchAppStore
	other := searchPlay
	if store == "play" || store == "android" {
		search, other = searchPlay, searchAppStore
	}
	var out []App
	var lastErr error
	seen := map[string]bool{}
	for _, t := range terms {
		apps, err := search(ctx, t, per)
		if err != nil || len(apps) == 0 {
			apps, err = other(ctx, t, per)
		}
		if err != nil {
			lastErr = err
			continue
		}
		for _, a := range apps {
			if !seen[a.Page] && len(a.Shots) > 0 {
				seen[a.Page] = true
				out = append(out, a)
			}
		}
		if len(out) >= max(limit, len(terms)) {
			break
		}
	}
	if len(out) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("appshots: %w", lastErr)
		}
		return nil, fmt.Errorf("appshots: no app with screenshots matched %q; name real apps (\"Structured; Things 3\") or use plainer words", query)
	}
	return out, nil
}

var shotSize = regexp.MustCompile(`/\d+x\d+(bb|w)?\.(jpg|png|webp)$`)

func searchAppStore(ctx context.Context, term string, limit int) ([]App, error) {
	u := fmt.Sprintf("%s?term=%s&entity=software&country=us&limit=%d", itunesAPI, url.QueryEscape(term), min(limit+2, 25))
	var res struct {
		Results []struct {
			TrackName         string   `json:"trackName"`
			SellerName        string   `json:"sellerName"`
			PrimaryGenreName  string   `json:"primaryGenreName"`
			AverageUserRating float64  `json:"averageUserRating"`
			UserRatingCount   int      `json:"userRatingCount"`
			TrackViewURL      string   `json:"trackViewUrl"`
			ScreenshotUrls    []string `json:"screenshotUrls"`
		} `json:"results"`
	}
	if err := getJSONWith(ctx, u, nil, &res); err != nil {
		return nil, err
	}
	var out []App
	for _, r := range res.Results {
		if len(r.ScreenshotUrls) == 0 {
			continue
		}
		var shots []string
		for _, s := range r.ScreenshotUrls {
			// Ask for a phone-sized copy; the default is often 1290 wide.
			shots = append(shots, shotSize.ReplaceAllString(s, "/600x0w.jpg"))
		}
		out = append(out, App{Store: "App Store", Name: r.TrackName, Developer: r.SellerName, Genre: r.PrimaryGenreName, Rating: r.AverageUserRating, Ratings: r.UserRatingCount, Page: r.TrackViewURL, Shots: shots})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

var (
	playID    = regexp.MustCompile(`/store/apps/details\?id=([A-Za-z0-9._]+)`)
	playShot  = regexp.MustCompile(`<img[^>]+src="(https?://[^"=]+)=[^"]*"[^>]*alt="Screenshot image"`)
	playTitle = regexp.MustCompile(`itemprop="name">([^<]+)<`)
	playDev   = regexp.MustCompile(`href="/store/apps/dev(?:eloper)?\?id=[^"]+"[^>]*>(?:<span[^>]*>)?([^<]+)<`)
)

func searchPlay(ctx context.Context, term string, limit int) ([]App, error) {
	hdr := map[string]string{"User-Agent": browserUA, "Accept-Language": "en-US,en;q=0.9"}
	page, err := httpGetWith(ctx, fmt.Sprintf("%s/store/search?q=%s&c=apps&hl=en&gl=US", playBase, url.QueryEscape(term)), hdr, 8<<20)
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, m := range playID.FindAllStringSubmatch(string(page), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	var out []App
	for _, id := range ids {
		if len(out) >= limit {
			break
		}
		link := fmt.Sprintf("%s/store/apps/details?id=%s&hl=en&gl=US", playBase, id)
		body, err := httpGetWith(ctx, link, hdr, 8<<20)
		if err != nil {
			continue
		}
		s := string(body)
		a := App{Store: "Google Play", Page: fmt.Sprintf("https://play.google.com/store/apps/details?id=%s", id), Name: id}
		if m := playTitle.FindStringSubmatch(s); m != nil {
			a.Name = htmlText(m[1])
		}
		if m := playDev.FindStringSubmatch(s); m != nil {
			a.Developer = htmlText(m[1])
		}
		for _, m := range playShot.FindAllStringSubmatch(s, 8) {
			a.Shots = append(a.Shots, m[1]+"=h900")
		}
		if len(a.Shots) > 0 {
			out = append(out, a)
		}
	}
	return out, nil
}

func htmlText(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&#39;", "'", "&quot;", `"`, "&lt;", "<", "&gt;", ">")
	return strings.TrimSpace(r.Replace(s))
}

// AppSheet downloads up to perApp screenshots of each app and lays them out
// one app per row in a JPEG contact sheet saved under the project's refs
// folder. It returns the sheet's path and bytes.
func AppSheet(ctx context.Context, workDir, project, query string, apps []App, perApp int) (string, []byte, error) {
	if perApp <= 0 {
		perApp = 4
	}
	const cellH, gap, pad = 560, 16, 24
	type cell struct {
		row, col int
		img      image.Image
	}
	var (
		mu    sync.Mutex
		cells []cell
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 6)
	)
	hdr := map[string]string{"User-Agent": browserUA}
	for r, a := range apps {
		for c, u := range a.Shots {
			if c >= perApp {
				break
			}
			wg.Add(1)
			go func(r, c int, u string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				data, err := httpGetWith(ctx, u, hdr, 12<<20)
				if err != nil {
					return
				}
				img, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					return
				}
				mu.Lock()
				cells = append(cells, cell{r, c, img})
				mu.Unlock()
			}(r, c, u)
		}
	}
	wg.Wait()
	if len(cells) == 0 {
		return "", nil, fmt.Errorf("appshots: none of the screenshots downloaded")
	}
	// Scale every shot to one height; a landscape shot keeps its ratio.
	widths := map[int]int{}
	scaled := map[[2]int]image.Image{}
	for _, c := range cells {
		b := c.img.Bounds()
		w := max(1, b.Dx()*cellH/max(1, b.Dy()))
		if w > cellH*2 {
			w = cellH * 2
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, cellH))
		draw.CatmullRom.Scale(dst, dst.Bounds(), c.img, b, draw.Src, nil)
		scaled[[2]int{c.row, c.col}] = dst
	}
	rows := 0
	for r := range apps {
		x := pad
		has := false
		for c := 0; c < perApp; c++ {
			if im, ok := scaled[[2]int{r, c}]; ok {
				x += im.Bounds().Dx() + gap
				has = true
			}
		}
		if has {
			widths[r] = x - gap + pad
			rows++
		}
	}
	W := 0
	for _, w := range widths {
		W = max(W, w)
	}
	H := pad*2 + rows*cellH + (rows-1)*gap*2
	sheet := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{color.RGBA{0xe9, 0xec, 0xf1, 0xff}}, image.Point{}, draw.Src)
	y := pad
	for r := range apps {
		if _, ok := widths[r]; !ok {
			continue
		}
		x := pad
		for c := 0; c < perApp; c++ {
			im, ok := scaled[[2]int{r, c}]
			if !ok {
				continue
			}
			draw.Draw(sheet, image.Rect(x, y, x+im.Bounds().Dx(), y+cellH), im, image.Point{}, draw.Src)
			x += im.Bounds().Dx() + gap
		}
		y += cellH + gap*2
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sheet, &jpeg.Options{Quality: 82}); err != nil {
		return "", nil, err
	}
	dir := filepath.Join(Dir(workDir, project), "refs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	slug := design.Slug(query)
	if slug == "" {
		slug = "apps"
	}
	if len(slug) > 60 {
		slug = slug[:60]
	}
	path := filepath.Join(dir, slug+".jpg")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", nil, err
	}
	var listed []App
	for r, a := range apps {
		if _, ok := widths[r]; ok {
			listed = append(listed, a)
		}
	}
	if raw, err := json.MarshalIndent(listed, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, slug+".json"), raw, 0o644)
	}
	return path, buf.Bytes(), nil
}

// AppsWithRows returns the apps that made it onto a sheet, in row order.
func AppsWithRows(workDir, project, query string) []App {
	slug := design.Slug(query)
	if len(slug) > 60 {
		slug = slug[:60]
	}
	raw, err := os.ReadFile(filepath.Join(Dir(workDir, project), "refs", slug+".json"))
	if err != nil {
		return nil
	}
	var apps []App
	_ = json.Unmarshal(raw, &apps)
	return apps
}
