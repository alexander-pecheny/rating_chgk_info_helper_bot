package tg

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-telegram/bot/models"
)

// messageKeys are the four places an update can carry a message.
var messageKeys = []string{"message", "edited_message", "channel_post", "edited_channel_post"}

// maxRemembered bounds the raw-JSON cache if a middleware ever fails to claim
// an entry; the bot handles a few updates a minute, so this is never reached.
const maxRemembered = 512

// updateTap sits between the library and Telegram for two reasons.
//
// It keeps each update's original JSON, so the traffic log records what
// actually arrived rather than what the library's structs could represent.
//
// More importantly it rescues updates the library cannot parse. A RichBlock of
// an unknown type makes models.Update fail to unmarshal, which fails the whole
// getUpdates batch; because the offset only advances after a successful parse,
// the bot would refetch that batch forever and answer nobody. One block type
// added in a future Bot API would be enough to stop the bot dead. Here such a
// message is stripped to its flattened text instead, which is all the announce
// flow needs to relay it.
type updateTap struct {
	inner *http.Client

	mu  sync.Mutex
	raw map[int64]json.RawMessage
}

func newUpdateTap(inner *http.Client) *updateTap {
	return &updateTap{inner: inner, raw: map[int64]json.RawMessage{}}
}

func (t *updateTap) Do(request *http.Request) (*http.Response, error) {
	response, err := t.inner.Do(request)
	if err != nil || !strings.HasSuffix(request.URL.Path, "/getUpdates") {
		return response, err
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	body = t.sift(body)
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

// sift records every update's raw JSON and repairs, or as a last resort drops,
// the ones the library would choke on.
func (t *updateTap) sift(body []byte) []byte {
	var envelope struct {
		OK     bool              `json:"ok"`
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK {
		return body
	}

	rewritten := false
	kept := make([]json.RawMessage, 0, len(envelope.Result))
	for _, raw := range envelope.Result {
		var update models.Update
		if err := json.Unmarshal(raw, &update); err != nil {
			slog.Error("update did not parse, degrading it", "err", err)
			repaired, ok := degrade(raw)
			rewritten = true
			if !ok {
				continue
			}
			if err := json.Unmarshal(repaired, &update); err != nil {
				slog.Error("update did not parse even degraded, dropping it", "err", err)
				continue
			}
			raw = repaired
		}
		t.remember(update.ID, raw)
		kept = append(kept, raw)
	}
	if !rewritten {
		return body
	}
	repacked, err := json.Marshal(struct {
		OK     bool              `json:"ok"`
		Result []json.RawMessage `json:"result"`
	}{OK: true, Result: kept})
	if err != nil {
		return body
	}
	return repacked
}

// degrade replaces a message's rich_message with its flattened text, which is
// what every handler in this bot actually reads.
func degrade(raw json.RawMessage) (json.RawMessage, bool) {
	var update map[string]any
	if err := json.Unmarshal(raw, &update); err != nil {
		return nil, false
	}
	touched := false
	for _, key := range messageKeys {
		message, ok := update[key].(map[string]any)
		if !ok {
			continue
		}
		rich, ok := message["rich_message"].(map[string]any)
		if !ok {
			continue
		}
		blocks, _ := rich["blocks"].([]any)
		delete(message, "rich_message")
		if _, hasText := message["text"]; !hasText {
			message["text"] = flattenBlocks(blocks)
		}
		touched = true
	}
	if !touched {
		return nil, false
	}
	repaired, err := json.Marshal(update)
	if err != nil {
		return nil, false
	}
	return repaired, true
}

func (t *updateTap) remember(id int64, raw json.RawMessage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.raw) >= maxRemembered {
		clear(t.raw)
	}
	t.raw[id] = raw
}

// claim returns an update's original JSON and forgets it.
func (t *updateTap) claim(id int64) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, ok := t.raw[id]
	if !ok {
		return ""
	}
	delete(t.raw, id)
	return string(raw)
}
