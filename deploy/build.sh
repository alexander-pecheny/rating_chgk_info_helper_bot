#!/bin/sh
# Build the bot for the vps and copy it over. Nothing is compiled on the vps
# itself: the binary is static, so the box needs no Go toolchain and no cgo.
set -eu

HOST="${1:-vps2day-ee}"
TARGET="${2:-/home/ap/rating_chgk_info_helper_bot}"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
	-o rating_bot ./cmd/rating-bot

# Replacing a running binary in place fails with "text file busy", so the new
# one lands beside it and is moved once the service is down.
scp rating_bot "$HOST:$TARGET/rating_bot.new"
ssh "$HOST" "sudo systemctl stop rating_bot \
	&& mv $TARGET/rating_bot.new $TARGET/rating_bot \
	&& sudo systemctl start rating_bot \
	&& systemctl is-active rating_bot"
