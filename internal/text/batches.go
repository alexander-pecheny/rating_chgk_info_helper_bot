package text

import (
	"regexp"
	"strings"
)

// MaxLength is well under Telegram's own limit, leaving room for the tags a
// split has to reopen.
const MaxLength = 2048

var tagPattern = regexp.MustCompile(`<(/?)(\w+)(?:\s[^>]*)?>`)

// openTagsAt reports which HTML tags are still open at the end of a fragment.
func openTagsAt(fragment string) []string {
	var open []string
	for _, match := range tagPattern.FindAllStringSubmatch(fragment, -1) {
		name := strings.ToLower(match[2])
		if match[1] == "/" {
			if len(open) > 0 && open[len(open)-1] == name {
				open = open[:len(open)-1]
			}
			continue
		}
		open = append(open, name)
	}
	return open
}

func openingTags(tags []string) string {
	var b strings.Builder
	for _, tag := range tags {
		b.WriteString("<" + tag + ">")
	}
	return b.String()
}

func closingTags(tags []string) string {
	var b strings.Builder
	for i := len(tags) - 1; i >= 0; i-- {
		b.WriteString("</" + tags[i] + ">")
	}
	return b.String()
}

// safeSplitPoint prefers a paragraph break, then a line break, and failing
// both refuses to cut in the middle of a tag.
func safeSplitPoint(runes []rune, maxLen int) int {
	if len(runes) <= maxLen {
		return len(runes)
	}
	area := string(runes[:maxLen])
	if at := runeIndex(area, strings.LastIndex(area, "\n\n")); at > maxLen/2 {
		return at + 2
	}
	if at := runeIndex(area, strings.LastIndex(area, "\n")); at > maxLen/2 {
		return at + 1
	}
	lastTagEnd := runeIndex(area, strings.LastIndex(area, ">"))
	lastTagStart := runeIndex(area, strings.LastIndex(area, "<"))
	if lastTagStart > lastTagEnd {
		return lastTagStart
	}
	return maxLen
}

// runeIndex converts a byte offset from strings.LastIndex into a rune offset,
// so the whole splitter can count characters the way Telegram does.
func runeIndex(s string, byteOffset int) int {
	if byteOffset < 0 {
		return -1
	}
	return len([]rune(s[:byteOffset]))
}

// Batches cuts a message into Telegram-sized pieces, closing any tag left open
// at a cut and reopening it at the start of the next piece.
func Batches(message string) []string {
	var batches []string
	var carried []string
	rest := []rune(message)

	for len(rest) > 0 {
		prefix := openingTags(carried)
		if len(rest) <= MaxLength {
			batches = append(batches, prefix+string(rest))
			break
		}
		available := MaxLength - len([]rune(prefix))
		splitPoint := safeSplitPoint(rest, available)
		if splitPoint < 1 { // a pathological prefix must not stall the loop
			splitPoint = available
		}
		combined := prefix + string(rest[:splitPoint])
		stillOpen := openTagsAt(combined)
		batches = append(batches, combined+closingTags(stillOpen))
		carried = stillOpen
		rest = []rune(strings.TrimLeft(string(rest[splitPoint:]), "\n"))
	}
	return batches
}
