package business

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Link markers wrap the text of a link while a page is cleaned, so that the share of link text in a line is known
// after the address has been dropped.
const (
	linkStart = "\x01"
	linkEnd   = "\x02"
)

var (
	// image is Markdown for an image: ![alt](address). The alternative text of nested links may hold brackets, which
	// the repetition in removeImages takes care of.
	image = regexp.MustCompile(`!\[[^\[\]]*\]\((?:<[^>]*>|[^)]*)\)`)
	// link is Markdown for a link: [text](address "title"). The text may span lines.
	link = regexp.MustCompile(`\[([^\[\]]*)\]\((?:<[^>]*>|[^)\s]*)(?:\s+"[^"]*")?\)`)
	// placeholder is a template variable that was never filled, such as ${title}.
	placeholder = regexp.MustCompile(`\$\{[^}]*\}`)
	// separator is a line that is nothing but a horizontal rule or a run of punctuation.
	separator = regexp.MustCompile(`^[\s*_\-=~·•|\\]+$`)
	// byline is an author or agency abbreviation in brackets, such as ( mro) or (dpa).
	byline = regexp.MustCompile(`^\(\s*[\p{L}.]{1,8}\s*\)$`)
	// sentenceEnd is how a sentence ends in the scripts of the world's major languages, possibly inside quotation
	// marks or a bracket: a full stop, question or exclamation mark, ellipsis, colon or semicolon, their full-width,
	// Arabic and Devanagari forms.
	sentenceEnd = regexp.MustCompile(`[.!?…:;。！？．؟।]["'“”„»«」』)\]]*$`)
	// blankRun is a gap of more than one empty line.
	blankRun = regexp.MustCompile(`\n{3,}`)
)

const (
	// labelMaxLength is the length in characters up to which a line that is no sentence, heading or list item is a
	// label: a menu entry, a button, an advertisement mark. Lines that carry content end in a sentence.
	labelMaxLength = 80
	// linkListMaxLength is the length up to which a line that is mostly link without a sentence is a teaser or menu
	// entry.
	linkListMaxLength = 200
	// signMaxLength is the length up to which a line is a stray word or sign, such as a close button.
	signMaxLength = 4
	// linkListShare is the share of link text in a line above which the line is a list of links.
	linkListShare = 0.6
)

// CleanPageText reduces the Markdown of a crawled page to roughly the article body: images are removed, links shrink
// to their text and lines that are menus, logins, advertising, tracking or teasers are dropped. It goes by the shape
// of the lines, so one description fits every site and every language. A short line seen before is dropped.
func CleanPageText(markdown string) string {
	text := removeImages(markdown)
	text = link.ReplaceAllString(text, linkStart+"$1"+linkEnd)
	// A link inside a link text, as left by images that were removed, leaves empty markers behind.
	text = strings.ReplaceAll(text, linkStart+linkEnd, "")

	seen := make(map[string]bool)
	var kept []string
	dropped := 0
	for _, raw := range strings.Split(text, "\n") {
		line, linkShare := lineAndLinkShare(raw)
		if line == "" {
			kept = append(kept, "")
			continue
		}
		if reason := noiseOf(line, linkShare); reason != "" {
			slog.Debug("Dropping line of page", "reason", reason, "line", line)
			dropped++
			continue
		}
		if utf8.RuneCountInString(line) < labelMaxLength && seen[line] {
			slog.Debug("Dropping repeated line of page", "line", line)
			dropped++
			continue
		}
		seen[line] = true
		kept = append(kept, line)
	}
	cleaned := strings.TrimSpace(blankRun.ReplaceAllString(strings.Join(kept, "\n"), "\n\n"))
	slog.Debug("Cleaned page", "before", len(markdown), "after", len(cleaned), "droppedLines", dropped)
	return cleaned
}

// removeImages removes images until none is left; removing the inner one of nested images exposes the outer.
func removeImages(text string) string {
	for {
		next := image.ReplaceAllString(text, "")
		if next == text {
			return text
		}
		text = next
	}
}

// lineAndLinkShare strips the link markers from a line and tells which share of its characters was link text.
func lineAndLinkShare(raw string) (string, float64) {
	raw = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(raw), `\`))
	var linkChars int
	var plain strings.Builder
	inLink := false
	for _, r := range raw {
		switch string(r) {
		case linkStart:
			inLink = true
		case linkEnd:
			inLink = false
		default:
			plain.WriteRune(r)
			if inLink {
				linkChars++
			}
		}
	}
	line := strings.Join(strings.Fields(plain.String()), " ")
	if line == "" {
		return "", 0
	}
	return line, float64(linkChars) / float64(utf8.RuneCountInString(plain.String()))
}

// noiseOf says why a line is something around the article and not part of it, or returns "" for a line that stays.
// It goes by the shape of the line only: a line that is no sentence is a label when it is short and not a heading or
// list item, and a list of links when most of it is link text.
func noiseOf(line string, linkShare float64) string {
	bare := strings.TrimLeft(line, "-*+#> ")
	length := utf8.RuneCountInString(line)
	switch {
	case placeholder.MatchString(line):
		return "unfilled template variable"
	case separator.MatchString(line):
		return "separator"
	case utf8.RuneCountInString(bare) <= signMaxLength:
		return "stray sign"
	case byline.MatchString(line):
		return "byline"
	case sentenceEnd.MatchString(line):
		return ""
	case length <= linkListMaxLength && linkShare >= linkListShare:
		return "list of links"
	case length <= labelMaxLength && bare == line:
		return "label"
	}
	return ""
}

// CutText cuts text to at most limit characters, ending at a whole word when it can.
func CutText(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	cut := string([]rune(text)[:limit])
	if i := strings.LastIndexAny(cut, " \n\t"); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n\t")
}
