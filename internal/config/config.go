package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DataDir           string
	Token             string
	Admins            map[int64]bool
	AnnounceChannelID int64
	Debug             bool
}

func (c Config) BotDB() string  { return filepath.Join(c.DataDir, "bot.db") }
func (c Config) LogsDB() string { return filepath.Join(c.DataDir, "logs.db") }

func (c Config) IsAdmin(chatID int64) bool { return c.Admins[chatID] }

type file struct {
	Admins            []int64 `json:"admins"`
	AnnounceChannelID int64   `json:"announce_channel_id"`
}

func Load(dataDir, tokenPath string, debug bool) (Config, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	var parsed file
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Config{}, err
	}
	if tokenPath == "" {
		tokenPath = filepath.Join(dataDir, "token")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return Config{}, err
	}
	admins := make(map[int64]bool, len(parsed.Admins))
	for _, id := range parsed.Admins {
		admins[id] = true
	}
	return Config{
		DataDir:           dataDir,
		Token:             strings.TrimSpace(string(token)),
		Admins:            admins,
		AnnounceChannelID: parsed.AnnounceChannelID,
		Debug:             debug,
	}, nil
}
