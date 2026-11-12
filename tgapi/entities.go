package tgapi

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// This file converts Telegram message entities
// (https://core.telegram.org/bots/api#messageentity) into Discord markdown.
//
// Telegram offsets and lengths are measured in UTF-16 code units, so the text
// is walked with a parallel UTF-16 position counter. Entities are normalized
// first (clamped to the text, merged when they overlap, de-nested inside
// code/pre) and then rendered as opening and closing markers, which keeps
// nested, adjacent and overlapping entities deterministic. Discord punctuation
// is backslash-escaped in normal text but left intact inside code/pre
// fragments, bare url/email entities and text link destinations.

// needEscape lists the ASCII punctuation Discord's markdown treats specially.
var needEscape = map[rune]bool{
	'\\': true,
	'_':  true, '*': true, '[': true, ']': true, '(': true, ')': true,
	'~': true, '`': true, '>': true, '#': true, '+': true, '-': true,
	'=': true, '|': true, '{': true, '}': true, '.': true, '!': true,
}

const quotePrefix = "> "

type rangeKind int

const (
	kindPlain rangeKind = iota // known but rendered as-is (mention, hashtag, ...)
	kindBold
	kindBoldItalic
	kindItalic
	kindUnderline
	kindStrikethrough
	kindSpoiler
	kindCode
	kindPre
	kindTextLink
	kindBlockquote
)

// entityRange is a Telegram entity normalized to UTF-16 offsets.
type entityRange struct {
	kind     rangeKind
	start    int    // UTF-16 offset, inclusive
	end      int    // UTF-16 offset, exclusive
	language string // for pre
	url      string // for text_link, sanitized
	noEscape bool   // do not backslash-escape characters inside
	index    int    // original entity order, for deterministic tie-breaking
}

// EntitiesToDiscordMarkdown converts plain text with entities to Discord
// markdown, escaping Discord punctuation outside of code/pre fragments, bare
// url/email entities and text link destinations.
func EntitiesToDiscordMarkdown(text string, messageEntities []tgbotapi.MessageEntity) string {
	runes := []rune(text)

	// pos[i] holds the UTF-16 offset of runes[i]; the last element is the
	// UTF-16 length of the whole text.
	pos := make([]int, len(runes)+1)
	units := 0
	for i, r := range runes {
		pos[i] = units
		if n := utf16.RuneLen(r); n > 0 {
			units += n
		} else {
			units++ // lone surrogate: count one unit like utf16.Encode would
		}
	}
	pos[len(runes)] = units
	total := units

	// Per-UTF-16-position rendering events.
	closes := make(map[int]string)
	opens := make(map[int]string)
	quotes := make(map[int]bool) // positions that need a quote line prefix
	skipEscape := make([]bool, total+1)

	for _, r := range normalizeEntities(messageEntities, total) {
		if r.noEscape {
			for u := r.start; u < r.end; u++ {
				skipEscape[u] = true
			}
		}
		if r.kind == kindBlockquote {
			for _, p := range quoteLineStarts(r, runes, pos) {
				quotes[p] = true
			}
			continue
		}
		// Ranges are sorted by (start, end desc), so appending opens and
		// prepending closes yields correct nesting at shared positions.
		opens[r.start] += r.openMarker()
		closes[r.end] = r.closeMarker() + closes[r.end]
	}

	var b strings.Builder
	b.Grow(len(text) + 16*len(opens))
	for i, r := range runes {
		p := pos[i]
		b.WriteString(closes[p])
		if quotes[p] {
			b.WriteString(quotePrefix)
		}
		b.WriteString(opens[p])
		if !skipEscape[p] && needEscape[r] {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	end := pos[len(runes)]
	b.WriteString(closes[end])
	if quotes[end] {
		b.WriteString(quotePrefix)
	}
	b.WriteString(opens[end])
	return b.String()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func classifyEntity(e *tgbotapi.MessageEntity) rangeKind {
	switch e.Type {
	case "bold":
		return kindBold
	case "italic":
		return kindItalic
	case "underline":
		return kindUnderline
	case "strikethrough":
		return kindStrikethrough
	case "spoiler":
		return kindSpoiler
	case "code":
		return kindCode
	case "pre":
		return kindPre
	case "text_link":
		return kindTextLink
	case "blockquote", "expandable_blockquote":
		return kindBlockquote
	default:
		return kindPlain
	}
}

func normalizeEntities(messageEntities []tgbotapi.MessageEntity, total int) []entityRange {
	ranges := make([]entityRange, 0, len(messageEntities))
	for i := range messageEntities {
		e := &messageEntities[i]
		start := clamp(e.Offset, 0, total)
		end := clamp(e.Offset+e.Length, start, total)
		if end <= start {
			continue
		}
		r := entityRange{
			kind:     classifyEntity(e),
			start:    start,
			end:      end,
			language: e.Language,
			url:      sanitizeDiscordURL(e.URL),
			index:    i,
		}
		switch r.kind {
		case kindCode, kindPre:
			r.noEscape = true
		case kindTextLink:
			if r.url == "" {
				r.kind = kindPlain
			}
		case kindPlain:
			r.noEscape = e.Type == "url" || e.Type == "email"
		}
		ranges = append(ranges, r)
	}

	ranges = mergeRanges(ranges)

	// Telegram never nests formatting inside code/pre; drop any stragglers so
	// their markers do not show up as literal text.
	filtered := ranges[:0]
	for _, r := range ranges {
		if insideCode(r, ranges) {
			continue
		}
		filtered = append(filtered, r)
	}

	return combineBoldItalic(filtered)
}

// mergeKey identifies ranges that render identically and can be merged.
func mergeKey(r entityRange) string {
	return fmt.Sprintf("%d\x00%s\x00%s", r.kind, r.language, r.url)
}

// mergeRanges folds touching or overlapping ranges of the same kind into one,
// so e.g. two adjacent bold runs do not produce a broken "****" sequence.
func mergeRanges(ranges []entityRange) []entityRange {
	if len(ranges) < 2 {
		return ranges
	}
	sortRanges(ranges)
	merged := make([]entityRange, 0, len(ranges))
	cur := ranges[0]
	for _, next := range ranges[1:] {
		if mergeKey(cur) == mergeKey(next) && next.start <= cur.end {
			if next.end > cur.end {
				cur.end = next.end
			}
			continue
		}
		merged = append(merged, cur)
		cur = next
	}
	return append(merged, cur)
}

func insideCode(r entityRange, all []entityRange) bool {
	for _, cp := range all {
		if cp.index == r.index {
			continue
		}
		if (cp.kind == kindCode || cp.kind == kindPre) && cp.start <= r.start && r.end <= cp.end {
			return true
		}
	}
	return false
}

// combineBoldItalic merges a bold and an italic entity covering the exact same
// range into a single ***-style range; adjacent or partially overlapping pairs
// already render correctly as-is.
func combineBoldItalic(ranges []entityRange) []entityRange {
	type span struct{ start, end int }
	groups := make(map[span][]entityRange)
	out := make([]entityRange, 0, len(ranges))
	for _, r := range ranges {
		switch r.kind {
		case kindBold, kindItalic:
			s := span{r.start, r.end}
			groups[s] = append(groups[s], r)
		default:
			out = append(out, r)
		}
	}
	for _, rs := range groups {
		var bold, italic *entityRange
		for i := range rs {
			switch rs[i].kind {
			case kindBold:
				bold = &rs[i]
			case kindItalic:
				italic = &rs[i]
			}
		}
		switch {
		case bold != nil && italic != nil:
			out = append(out, entityRange{
				kind:  kindBoldItalic,
				start: bold.start,
				end:   bold.end,
				index: min(bold.index, italic.index),
			})
		case bold != nil:
			out = append(out, *bold)
		case italic != nil:
			out = append(out, *italic)
		}
	}
	sortRanges(out)
	return out
}

func sortRanges(rs []entityRange) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end > b.end
		}
		// Keep text links outermost when they span the exact same range.
		aLink, bLink := a.kind == kindTextLink, b.kind == kindTextLink
		if aLink != bLink {
			return aLink
		}
		return a.index < b.index
	})
}

// quoteLineStarts returns the UTF-16 positions where a quote prefix must be
// inserted so every line of the blockquote range is quoted.
func quoteLineStarts(r entityRange, runes []rune, pos []int) []int {
	starts := []int{r.start}
	for i := range runes {
		if runes[i] == '\n' && pos[i] >= r.start && pos[i] < r.end && pos[i+1] < r.end {
			starts = append(starts, pos[i+1])
		}
	}
	return starts
}

// sanitizeDiscordURL percent-encodes characters that would otherwise terminate
// the [text](url) construct early.
func sanitizeDiscordURL(u string) string {
	if u == "" {
		return ""
	}
	return strings.NewReplacer(
		"(", "%28", ")", "%29", " ", "%20", "\n", "", "\r", "",
	).Replace(u)
}

func (r entityRange) openMarker() string {
	switch r.kind {
	case kindBold:
		return "**"
	case kindBoldItalic:
		return "***"
	case kindItalic:
		return "*"
	case kindUnderline:
		return "__"
	case kindStrikethrough:
		return "~~"
	case kindSpoiler:
		return "||"
	case kindCode:
		return "`"
	case kindPre:
		return "```" + r.language + "\n"
	case kindTextLink:
		return "["
	default:
		return ""
	}
}

// closeMarker equals openMarker for every symmetric kind; pre and text_link
// close differently than they open.
func (r entityRange) closeMarker() string {
	switch r.kind {
	case kindPre:
		return "\n```"
	case kindTextLink:
		return "](" + r.url + ")"
	default:
		return r.openMarker()
	}
}
