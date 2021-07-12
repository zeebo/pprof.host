package main

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"time"

	"github.com/zeebo/errs/v2"
)

var enc = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz")

func pad(x string) string   { return x + "========"[:(8-len(x)%8)%8] }
func unpad(x string) string { return strings.TrimRight(x, "=") }

func idToName(id uint64) string {
	var buf [binary.MaxVarintLen64]byte
	return unpad(enc.EncodeToString(buf[:binary.PutUvarint(buf[:], id)]))
}

type record struct {
	name    string
	size    int
	created time.Time
}

type dbStore struct {
	db *sql.DB
}

func (d *dbStore) Init(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS profiles (
		id integer primary key,
		data,
		created integer
	)`)
	return errs.Wrap(err)
}

func (d *dbStore) Save(ctx context.Context, data []byte) (string, error) {
	var id uint64
	err := d.db.QueryRowContext(ctx,
		"INSERT INTO profiles(data, created) VALUES(?, ?) RETURNING id",
		data, time.Now().Unix(),
	).Scan(&id)
	return idToName(id), errs.Wrap(err)
}

func (d *dbStore) Load(ctx context.Context, name string) (data []byte, err error) {
	buf, err := enc.DecodeString(pad(name))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	id, n := binary.Uvarint(buf)
	if n <= 0 {
		return nil, errs.Errorf("invalid name: %q", name)
	}
	err = d.db.QueryRowContext(ctx,
		"SELECT data FROM profiles WHERE id = ?",
		id,
	).Scan(&data)
	return data, errs.Wrap(err)
}

func (d *dbStore) Recent(ctx context.Context, n int) (recs []record, err error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT id, length(data), created FROM profiles ORDER BY created DESC LIMIT ?",
		n,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, size, created uint64
		if err := rows.Scan(&id, &size, &created); err != nil {
			return nil, errs.Wrap(err)
		}
		recs = append(recs, record{
			name:    idToName(id),
			size:    int(size),
			created: time.Unix(int64(created), 0),
		})
	}
	return recs, errs.Combine(errs.Wrap(rows.Err()), errs.Wrap(rows.Close()))
}
