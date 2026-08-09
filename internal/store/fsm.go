package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// IdleTTL is how long a half-finished conversation stays alive. Past it the
// state reads as absent, which is what stops a forgotten /announce from
// swallowing an unrelated message next week.
const IdleTTL = 300 * time.Second

// Key identifies one conversation. The shape is the Python bot's, so an
// in-flight conversation survives the switch to this binary.
type Key struct {
	BotID    int64
	ChatID   int64
	UserID   int64
	ThreadID int64
	Destiny  string
}

// State is a conversation's position plus whatever the flow collected so far.
type State struct {
	Name string
	Data map[string]any
}

func (s State) String(key string) string {
	value, _ := s.Data[key].(string)
	return value
}

// Numbers come back from JSON as float64; every number these flows store is a
// day count that fits an int exactly.
func (s State) Int(key string) int {
	value, _ := s.Data[key].(float64)
	return int(value)
}

const fsmWhere = `bot_id=? and chat_id=? and user_id=? and thread_id=? and destiny=?`

func (k Key) args() []any {
	return []any{k.BotID, k.ChatID, k.UserID, k.ThreadID, k.Destiny}
}

type FSM struct {
	db  *DB
	ttl time.Duration
}

func NewFSM(db *DB, ttl time.Duration) *FSM { return &FSM{db: db, ttl: ttl} }

func (f *FSM) Get(key Key) State {
	var (
		name      sql.NullString
		data      []byte
		updatedAt float64
	)
	err := f.db.conn.QueryRow(
		`select state, data, updated_at from fsm_state where `+fsmWhere, key.args()...,
	).Scan(&name, &data, &updatedAt)
	if err != nil {
		return State{Data: map[string]any{}}
	}
	if time.Since(time.Unix(0, int64(updatedAt*float64(time.Second)))) > f.ttl {
		f.Clear(key)
		return State{Data: map[string]any{}}
	}
	state := State{Name: name.String, Data: map[string]any{}}
	if err := json.Unmarshal(data, &state.Data); err != nil {
		state.Data = map[string]any{}
	}
	return state
}

func (f *FSM) Set(key Key, state State) error {
	if state.Name == "" && len(state.Data) == 0 {
		return f.Clear(key)
	}
	data, err := json.Marshal(state.Data)
	if err != nil {
		return err
	}
	var name any
	if state.Name != "" {
		name = state.Name
	}
	args := append(key.args(), name, data, float64(time.Now().UnixNano())/float64(time.Second))
	_, err = f.db.conn.Exec(
		`insert into fsm_state
		 (bot_id, chat_id, user_id, thread_id, destiny, state, data, updated_at)
		 values (?,?,?,?,?,?,?,?)
		 on conflict(bot_id, chat_id, user_id, thread_id, destiny) do update set
		 state = excluded.state, data = excluded.data, updated_at = excluded.updated_at`,
		args...,
	)
	return err
}

// SetName moves the conversation to a new step, keeping what it has collected.
func (f *FSM) SetName(key Key, name string) error {
	state := f.Get(key)
	state.Name = name
	return f.Set(key, state)
}

func (f *FSM) Update(key Key, values map[string]any) error {
	state := f.Get(key)
	for k, v := range values {
		state.Data[k] = v
	}
	return f.Set(key, state)
}

func (f *FSM) Clear(key Key) error {
	_, err := f.db.conn.Exec(`delete from fsm_state where `+fsmWhere, key.args()...)
	return err
}
