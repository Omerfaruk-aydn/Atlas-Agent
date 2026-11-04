package voice

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Phrases are the fixed sentences the gateway says itself. They are never
// produced by the model, so they cannot leak internal detail.
type Phrases struct {
	Greeting       string
	Goodbye        string
	Thinking       string
	AgentError     string
	ProviderError  string
	Permission     string
	ConfirmPrompt  string // %s is the action summary
	Declined       string
	Silence        string
	TimeUp         string
	CodeOmitted    string
	TableOmitted   string
	MoreItems      string // %d is the number of items left out
	Link           string
	NotUnderstood  string
	BudgetExceeded string
	SiteDown       string // %s site, %s error code
	SiteRecovered  string // %s site
}

var phraseTable = map[string]Phrases{
	"tr": {
		Greeting:       "Merhaba, ben Atlas. Seni dinliyorum.",
		Goodbye:        "Görüşmek üzere.",
		Thinking:       "Bir saniye, bakıyorum.",
		AgentError:     "Üzgünüm, bu isteği şu an tamamlayamadım.",
		ProviderError:  "Ses hizmetinde bir sorun oluştu, görüşmeyi kapatıyorum.",
		Permission:     "Bu işlem için izin gerekiyor; telefondan onaylayamıyorum. Bilgisayardan devam edebilirsin.",
		ConfirmPrompt:  "Onay gerekiyor: %s. Onaylıyor musun? Evet ya da hayır de.",
		Declined:       "Tamam, yapmıyorum.",
		Silence:        "Seni duyamıyorum, görüşmeyi kapatıyorum.",
		TimeUp:         "Görüşme süresi doldu, kapatıyorum.",
		CodeOmitted:    "Kod kısmını sesli okumuyorum.",
		TableOmitted:   "Tabloyu sesli okumuyorum.",
		MoreItems:      "ve %d madde daha.",
		Link:           "bağlantı",
		NotUnderstood:  "Anlayamadım, tekrar söyler misin?",
		BudgetExceeded: "Bugünkü görüşme süresi doldu.",
		SiteDown:       "%s sitesine ulaşılamıyor, hata: %s.",
		SiteRecovered:  "%s sitesi yeniden çalışıyor.",
	},
	"en": {
		Greeting:       "Hi, this is Atlas. I'm listening.",
		Goodbye:        "Talk to you later.",
		Thinking:       "One moment, let me check.",
		AgentError:     "Sorry, I couldn't finish that right now.",
		ProviderError:  "The voice service had a problem, so I'm ending the call.",
		Permission:     "That needs a permission I can't grant over the phone. You can continue on your computer.",
		ConfirmPrompt:  "Confirmation needed: %s. Do you confirm? Say yes or no.",
		Declined:       "Okay, I won't do it.",
		Silence:        "I can't hear you, so I'm ending the call.",
		TimeUp:         "We're out of time for this call. Goodbye.",
		CodeOmitted:    "I'm skipping the code.",
		TableOmitted:   "I'm skipping the table.",
		MoreItems:      "and %d more.",
		Link:           "a link",
		NotUnderstood:  "Sorry, could you say that again?",
		BudgetExceeded: "Today's call time is used up.",
		SiteDown:       "%s is unreachable, error: %s.",
		SiteRecovered:  "%s is back up.",
	},
}

// PhrasesFor returns the phrases for a BCP-47 language, English when the
// language has none.
func PhrasesFor(language string) Phrases {
	lang, _, _ := strings.Cut(strings.ToLower(language), "-")
	if p, ok := phraseTable[lang]; ok {
		return p
	}
	return phraseTable["en"]
}

var (
	fenceRE      = regexp.MustCompile("(?s)```.*?(```|$)")
	inlineCodeRE = regexp.MustCompile("`([^`]*)`")
	linkRE       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	urlRE        = regexp.MustCompile(`https?://\S+`)
	emphasisRE   = regexp.MustCompile(`(\*\*|__|\*|~~)`)
	listItemRE   = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
	headingRE    = regexp.MustCompile(`^\s{0,3}#{1,6}\s*`)
	spaceRE      = regexp.MustCompile(`[ \t]+`)
)

// maxSpokenListItems is how many list items are read before summarizing.
const maxSpokenListItems = 3

// ToSpoken turns a model answer into text fit for speech: no code blocks,
// tables, markup or raw URLs, long lists shortened, at most maxRunes long.
// It removes and shortens but never rewrites words, so numbers and dates
// pass through unchanged.
func ToSpoken(text string, p Phrases, maxRunes int) string {
	text = fenceRE.ReplaceAllString(text, "\n"+p.CodeOmitted+"\n")
	text = linkRE.ReplaceAllString(text, "$1")
	text = urlRE.ReplaceAllString(text, p.Link)
	text = inlineCodeRE.ReplaceAllString(text, "$1")

	var out []string
	listed, skipped, inTable := 0, 0, false
	flushList := func() {
		if skipped > 0 {
			out = append(out, fmt.Sprintf(p.MoreItems, skipped))
		}
		listed, skipped = 0, 0
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") {
			if !inTable {
				flushList()
				out = append(out, p.TableOmitted)
				inTable = true
			}
			continue
		}
		inTable = false
		if trimmed == "" || strings.Trim(trimmed, "-=*_") == "" {
			continue
		}
		if listItemRE.MatchString(line) {
			listed++
			if listed > maxSpokenListItems {
				skipped++
				continue
			}
			line = listItemRE.ReplaceAllString(line, "")
			line = ensureSentence(strings.TrimSpace(line))
		} else {
			flushList()
		}
		line = headingRE.ReplaceAllString(line, "")
		line = emphasisRE.ReplaceAllString(line, "")
		line = spaceRE.ReplaceAllString(strings.TrimSpace(line), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	flushList()
	spoken := strings.Join(out, " ")
	return truncateAtSentence(spoken, maxRunes)
}

func ensureSentence(s string) string {
	if s == "" {
		return s
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	if strings.ContainsRune(".!?…:;", r) {
		return s
	}
	return s + "."
}

func truncateAtSentence(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	cut := string([]rune(s)[:maxRunes])
	// Prefer ending at the last complete sentence.
	for i := len(cut) - 1; i > 0; i-- {
		if strings.IndexByte(".!?", cut[i]) >= 0 && (i+1 == len(cut) || cut[i+1] == ' ') {
			return cut[:i+1]
		}
	}
	if i := strings.LastIndexFunc(cut, unicode.IsSpace); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}

const (
	minSegmentRunes = 24
	maxSegmentRunes = 280
)

// SplitSegments splits text into sentence-sized pieces for streaming
// synthesis. A period between digits (10.30, 3.5) never splits, short
// sentences are merged and long ones are split at commas or spaces.
func SplitSegments(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	var raw []string
	start := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r != '.' && r != '!' && r != '?' && r != '…' && r != '\n' {
			continue
		}
		if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
			continue
		}
		raw = append(raw, string(runes[start:i+1]))
		start = i + 1
	}
	if start < len(runes) {
		raw = append(raw, string(runes[start:]))
	}

	var merged []string
	var cur strings.Builder
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(s)
		if utf8.RuneCountInString(cur.String()) >= minSegmentRunes {
			merged = append(merged, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		if len(merged) > 0 && utf8.RuneCountInString(cur.String()) < minSegmentRunes {
			merged[len(merged)-1] += " " + cur.String()
		} else {
			merged = append(merged, cur.String())
		}
	}

	var out []string
	for _, s := range merged {
		out = append(out, splitLong(s)...)
	}
	return out
}

func splitLong(s string) []string {
	var out []string
	for utf8.RuneCountInString(s) > maxSegmentRunes {
		runes := []rune(s)
		window := string(runes[:maxSegmentRunes])
		cut := strings.LastIndex(window, ", ")
		if cut < maxSegmentRunes/3 {
			cut = strings.LastIndexFunc(window, unicode.IsSpace)
		}
		if cut <= 0 {
			cut = len(window)
		} else {
			cut++
		}
		out = append(out, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		out = append(out, s)
	}
	return out
}
