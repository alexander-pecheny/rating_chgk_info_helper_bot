// Package tg is the bot itself: how an update is routed, what each command
// answers, and what the scheduled jobs send.
package tg

import (
	"encoding/json"
	"strings"

	"github.com/go-telegram/bot/models"
)

// textKeys are the keys whose string values a human reads. Everything else in
// a block is structure: block types, file ids, dimensions, emoji ids.
var textKeys = map[string]bool{
	"text": true, "caption": true, "alternative_text": true, "title": true,
}

// collectStrings walks any decoded JSON and gathers the prose out of it. A
// rich message carries no flat text, and every future block type is still a
// tree of the same four keys, so this needs no knowledge of block types.
func collectStrings(node any, isText bool, out *strings.Builder) {
	switch value := node.(type) {
	case string:
		if isText {
			out.WriteString(value)
		}
	case []any:
		for _, item := range value {
			collectStrings(item, isText, out)
		}
	case map[string]any:
		for key, item := range value {
			collectStrings(item, textKeys[key], out)
		}
	}
}

// flattenBlocks renders one line per block, which is enough to log a rich
// message and to measure how long it is.
func flattenBlocks(blocks []any) string {
	var lines []string
	for _, block := range blocks {
		var line strings.Builder
		collectStrings(block, false, &line)
		if trimmed := strings.TrimSpace(line.String()); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

// richMessageText flattens a rich message by round-tripping it through JSON,
// so the walk above sees exactly what Telegram sent.
func richMessageText(rich *models.RichMessage) string {
	if rich == nil {
		return ""
	}
	encoded, err := json.Marshal(rich.Blocks)
	if err != nil {
		return ""
	}
	var blocks []any
	if err := json.Unmarshal(encoded, &blocks); err != nil {
		return ""
	}
	return flattenBlocks(blocks)
}

// MessageText is the prose of a message, wherever Telegram chose to put it.
func MessageText(message *models.Message) string {
	if message == nil {
		return ""
	}
	if message.Text != "" {
		return message.Text
	}
	if message.Caption != "" {
		return message.Caption
	}
	return richMessageText(message.RichMessage)
}
