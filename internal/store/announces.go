package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Announce is one post the bot relayed into the announce channel, kept so
// its author can later delete or replace it.
type Announce struct {
	ID         int64
	MessageIDs []int
}

func (d *DB) RecordAnnounce(authorID int64, messageIDs []int, sentAt time.Time) error {
	encoded, err := json.Marshal(messageIDs)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(
		"insert into announces(author_id, message_ids, sent_at) values (?,?,?)",
		authorID, encoded, sentAt.Unix(),
	)
	return err
}

// LastAnnounce returns nil when the author has sent nothing since then.
func (d *DB) LastAnnounce(authorID int64, since time.Time) (*Announce, error) {
	return d.scanAnnounce(d.conn.QueryRow(
		`select id, message_ids from announces where author_id = ? and sent_at >= ?
		 order by sent_at desc, id desc limit 1`,
		authorID, since.Unix(),
	))
}

func (d *DB) scanAnnounce(row *sql.Row) (*Announce, error) {
	var (
		announce Announce
		raw      []byte
	)
	err := row.Scan(&announce.ID, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &announce.MessageIDs); err != nil {
		return nil, err
	}
	return &announce, nil
}

func (d *DB) DeleteAnnounce(id int64) error {
	_, err := d.conn.Exec("delete from announces where id = ?", id)
	return err
}

// Announce returns nil when the author has no post by that id, which is
// also the answer once it has been deleted.
func (d *DB) Announce(authorID, id int64) (*Announce, error) {
	return d.scanAnnounce(d.conn.QueryRow(
		"select id, message_ids from announces where author_id = ? and id = ?", authorID, id))
}
