// Package store owns the two sqlite files: subscriber state in bot.db and
// message traffic in logs.db. Both schemas are the ones the Python bot wrote,
// so an existing deployment keeps its data across the rewrite.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
	_ "modernc.org/sqlite"
)

var botSchema = []string{
	`CREATE TABLE IF NOT EXISTS data (
    id integer PRIMARY KEY,
    name text,
    state text,
    chat_ids text,
    prefs text
)`,
	`CREATE TABLE IF NOT EXISTS chat_prefs (
    chat_id integer PRIMARY KEY,
    prefs text
)`,
	`CREATE TABLE IF NOT EXISTS banned_users (
    chat_id integer PRIMARY KEY
)`,
	`CREATE TABLE IF NOT EXISTS fsm_state (
    bot_id integer NOT NULL,
    chat_id integer NOT NULL,
    user_id integer NOT NULL,
    thread_id integer NOT NULL,
    destiny text NOT NULL,
    state text,
    data text NOT NULL,
    updated_at real NOT NULL,
    PRIMARY KEY (bot_id, chat_id, user_id, thread_id, destiny)
)`,
}

// Subscription records which of the three notification kinds a chat wants:
// "r" for new applications, "i" for controversials, "a" for appeals.
type Subscription map[string]int

func DefaultSubscription() Subscription  { return Subscription{"r": 1, "i": 1, "a": 1} }
func JuryOnlySubscription() Subscription { return Subscription{"r": 0, "i": 1, "a": 0} }

type Application struct {
	Status string `json:"status"`
	Rep    string `json:"rep"`
}

type Tournament struct {
	ID           int
	Name         string
	Applications map[string]Application
	Subscribers  map[int64]Subscription
}

type DB struct{ conn *sql.DB }

func open(path string, pragmas string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", "file:"+path+pragmas)
	if err != nil {
		return nil, err
	}
	// One writer at a time is all this bot ever needs, and it removes every
	// chance of a "database is locked" error between the jobs and a handler.
	conn.SetMaxOpenConns(1)
	return conn, nil
}

func Open(path string) (*DB, error) {
	conn, err := open(path, "?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	for _, statement := range botSchema {
		if _, err := conn.Exec(statement); err != nil {
			conn.Close()
			return nil, fmt.Errorf("bot schema: %w", err)
		}
	}
	return &DB{conn: conn}, nil
}

func (d *DB) Close() error { return d.conn.Close() }

func scanTournament(scan func(...any) error) (Tournament, error) {
	var (
		t       Tournament
		state   []byte
		chatIDs []byte
	)
	if err := scan(&t.ID, &t.Name, &state, &chatIDs); err != nil {
		return t, err
	}
	if err := json.Unmarshal(state, &t.Applications); err != nil {
		return t, fmt.Errorf("applications of tournament %d: %w", t.ID, err)
	}
	subscribers, err := ParseChatIDs(chatIDs)
	if err != nil {
		return t, fmt.Errorf("subscribers of tournament %d: %w", t.ID, err)
	}
	t.Subscribers = subscribers
	return t, nil
}

func (d *DB) Tournaments() ([]Tournament, error) {
	rows, err := d.conn.Query("select id, name, state, chat_ids from data")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tournament
	for rows.Next() {
		t, err := scanTournament(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Tournament returns nil when nobody watches that tournament yet.
func (d *DB) Tournament(id int) (*Tournament, error) {
	row := d.conn.QueryRow("select id, name, state, chat_ids from data where id = ?", id)
	t, err := scanTournament(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) AddTournament(t Tournament) error {
	applications, err := json.Marshal(t.Applications)
	if err != nil {
		return err
	}
	subscribers, err := SerializeChatIDs(t.Subscribers)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(
		"insert into data(id,name,state,chat_ids) values (?,?,?,?)",
		t.ID, t.Name, applications, subscribers,
	)
	return err
}

func (d *DB) SetSubscribers(tournamentID int, subscribers map[int64]Subscription) error {
	encoded, err := SerializeChatIDs(subscribers)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec("update data set chat_ids = ? where id = ?", encoded, tournamentID)
	return err
}

func (d *DB) SetApplications(tournamentID int, applications map[string]Application) error {
	encoded, err := json.Marshal(applications)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec("update data set state = ? where id = ?", encoded, tournamentID)
	return err
}

func (d *DB) DeleteTournament(id int) error {
	_, err := d.conn.Exec("delete from data where id = ?", id)
	return err
}

// Prefs are a chat's settings: its rating host and its date grid defaults.
type Prefs struct {
	Host  string          `json:"host,omitempty"`
	Dates json.RawMessage `json:"dates,omitempty"`
}

func (d *DB) Prefs(chatID int64) (Prefs, error) {
	var raw []byte
	err := d.conn.QueryRow("select prefs from chat_prefs where chat_id = ?", chatID).Scan(&raw)
	if err == sql.ErrNoRows {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	var prefs Prefs
	if err := json.Unmarshal(raw, &prefs); err != nil {
		return Prefs{}, err
	}
	return prefs, nil
}

// HostOf is the question almost every caller actually asks of the preferences.
func (d *DB) HostOf(chatID int64, fallback string) string {
	prefs, err := d.Prefs(chatID)
	if err != nil || prefs.Host == "" {
		return fallback
	}
	return prefs.Host
}

func (d *DB) SetPrefs(chatID int64, prefs Prefs) error {
	encoded, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(
		`insert into chat_prefs(chat_id, prefs) values (?, ?)
		 on conflict(chat_id) do update set prefs = excluded.prefs`,
		chatID, encoded,
	)
	return err
}

func (d *DB) BannedUsers() (map[int64]bool, error) {
	rows, err := d.conn.Query("select chat_id from banned_users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	banned := map[int64]bool{}
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, err
		}
		banned[chatID] = true
	}
	return banned, rows.Err()
}

func (d *DB) IsBanned(chatID int64) bool {
	var one int
	err := d.conn.QueryRow("select 1 from banned_users where chat_id = ?", chatID).Scan(&one)
	return err == nil
}

// Ban reports false when the chat was already banned.
func (d *DB) Ban(chatID int64) (bool, error) {
	if d.IsBanned(chatID) {
		return false, nil
	}
	_, err := d.conn.Exec("insert into banned_users(chat_id) values (?)", chatID)
	return err == nil, err
}

func (d *DB) Unban(chatID int64) (bool, error) {
	if !d.IsBanned(chatID) {
		return false, nil
	}
	_, err := d.conn.Exec("delete from banned_users where chat_id = ?", chatID)
	return err == nil, err
}

// ParseChatIDs reads the msgpack map the Python bot wrote into chat_ids.
func ParseChatIDs(raw []byte) (map[int64]Subscription, error) {
	if len(raw) == 0 {
		return map[int64]Subscription{}, nil
	}
	subscribers := map[int64]Subscription{}
	if err := msgpack.Unmarshal(raw, &subscribers); err != nil {
		return nil, err
	}
	return subscribers, nil
}

func SerializeChatIDs(subscribers map[int64]Subscription) ([]byte, error) {
	return msgpack.Marshal(subscribers)
}
