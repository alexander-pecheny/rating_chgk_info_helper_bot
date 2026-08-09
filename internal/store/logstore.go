package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

const (
	MaxLogBytes = 100 * 1024 * 1024
	pruneBatch  = 5000
)

var logSchema = []string{
	`CREATE TABLE IF NOT EXISTS traffic (
    id integer PRIMARY KEY AUTOINCREMENT,
    ts real NOT NULL,
    direction text NOT NULL,
    method text,
    chat_id integer,
    text text,
    payload text NOT NULL
)`,
	"CREATE INDEX IF NOT EXISTS traffic_ts ON traffic(ts)",
	"CREATE INDEX IF NOT EXISTS traffic_chat_id ON traffic(chat_id)",
	`CREATE TABLE IF NOT EXISTS app_log (
    id integer PRIMARY KEY AUTOINCREMENT,
    ts real NOT NULL,
    level text NOT NULL,
    logger text NOT NULL,
    message text NOT NULL
)`,
	"CREATE INDEX IF NOT EXISTS app_log_ts ON app_log(ts)",
}

// LogStore keeps message traffic and the application log queryable, capped at
// a size the vps can afford.
type LogStore struct {
	conn     *sql.DB
	path     string
	MaxBytes int64
}

func OpenLogStore(path string) (*LogStore, error) {
	// auto_vacuum must be set before any table exists to take effect at all.
	conn, err := open(path, "?_pragma=busy_timeout(5000)&_pragma=auto_vacuum(incremental)&_pragma=journal_mode(wal)")
	if err != nil {
		return nil, err
	}
	for _, statement := range logSchema {
		if _, err := conn.Exec(statement); err != nil {
			conn.Close()
			return nil, fmt.Errorf("log schema: %w", err)
		}
	}
	return &LogStore{conn: conn, path: path, MaxBytes: MaxLogBytes}, nil
}

func (s *LogStore) Close() error { return s.conn.Close() }

func seconds(t time.Time) float64 { return float64(t.UnixNano()) / float64(time.Second) }

func (s *LogStore) RecordTraffic(direction, method string, chatID int64, text, payload string) {
	var methodValue, textValue, chatValue any
	if method != "" {
		methodValue = method
	}
	if text != "" {
		textValue = text
	}
	if chatID != 0 {
		chatValue = chatID
	}
	_, err := s.conn.Exec(
		"insert into traffic(ts, direction, method, chat_id, text, payload) values (?,?,?,?,?,?)",
		seconds(time.Now()), direction, methodValue, chatValue, textValue, payload,
	)
	if err != nil {
		slog.Error("recording traffic", "direction", direction, "err", err)
	}
}

func (s *LogStore) RecordLog(ts time.Time, level, logger, message string) error {
	_, err := s.conn.Exec(
		"insert into app_log(ts, level, logger, message) values (?,?,?,?)",
		seconds(ts), level, logger, message,
	)
	return err
}

// TrafficRow is one recorded message, in or out.
type TrafficRow struct {
	Direction string
	Method    string
	ChatID    int64
	Text      string
}

// Traffic reads the recorded messages oldest first, which is what the sqlite
// queries in the README do by hand.
func (s *LogStore) Traffic() ([]TrafficRow, error) {
	rows, err := s.conn.Query(
		"select direction, coalesce(method,''), coalesce(chat_id,0), coalesce(text,'')" +
			" from traffic order by id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficRow
	for rows.Next() {
		var row TrafficRow
		if err := rows.Scan(&row.Direction, &row.Method, &row.ChatID, &row.Text); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountRows is how the tests check what pruning left behind.
func (s *LogStore) CountRows(table string) int64 {
	var count int64
	if err := s.conn.QueryRow("select count(*) from " + table).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *LogStore) Size() int64 {
	var pages, pageSize int64
	if err := s.conn.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		return 0
	}
	if err := s.conn.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0
	}
	return pages * pageSize
}

// Prune drops the oldest rows until the file fits under the cap.
func (s *LogStore) Prune() int64 {
	var removed int64
	for s.Size() > s.MaxBytes {
		before := s.Size()
		var batch int64
		for _, table := range []string{"traffic", "app_log"} {
			var count int64
			if err := s.conn.QueryRow("select count(*) from " + table).Scan(&count); err != nil || count == 0 {
				continue
			}
			limit := min(int64(pruneBatch), max(1, count/10))
			result, err := s.conn.Exec(fmt.Sprintf(
				"delete from %s where id in (select id from %s order by ts limit ?)", table, table), limit)
			if err != nil {
				slog.Error("pruning", "table", table, "err", err)
				continue
			}
			if affected, err := result.RowsAffected(); err == nil && affected > 0 {
				batch += affected
			}
		}
		s.reclaim()
		removed += batch
		if batch == 0 {
			break
		}
		if s.Size() >= before {
			// Deleting rows is not shrinking the file, so continuing would
			// empty both tables and still miss the cap. An oversized log is
			// the lesser problem; leave the rest and say so.
			slog.Error("log file will not shrink, giving up on this pass",
				"path", s.path, "bytes", before, "deleted", batch)
			break
		}
	}
	return removed
}

func (s *LogStore) reclaim() {
	var mode int
	if err := s.conn.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return
	}
	statement := "VACUUM"
	if mode == 2 {
		statement = "PRAGMA incremental_vacuum"
	}
	if _, err := s.conn.Exec(statement); err != nil {
		slog.Error("reclaiming log space", "err", err)
	}
}
