package tg

import (
	"context"
	_ "embed"

	"github.com/go-telegram/bot/models"

	"code.pecheny.me/pecheny/rating_chgk_info_helper_bot/internal/text"
)

//go:embed uvedoml.jpg
var sunsetImage []byte

// sunset answers a subscribe attempt now that the site's own bot sends these
// notifications: a screenshot of the site's settings page and the explanation.
func (b *Bot) sunset(ctx context.Context, message *models.Message) {
	b.sendPhoto(ctx, message.Chat.ID, "uvedoml.jpg", sunsetImage, text.Sunset)
}
