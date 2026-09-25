package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jkong7/cooked/internal/elo"
)

//go:embed schema.sql
var schema string

var (
	ErrNotFound  = errors.New("not found")
	ErrInvalid   = errors.New("invalid input")
	ErrNotYours  = errors.New("not your turn")
	ErrState     = errors.New("match is not in that state")
	ErrForbidden = errors.New("not allowed")
)

const (
	Rounds   = 3
	Turns    = Rounds * 2
	TurnTime = 45 * time.Second
	VoteTime = 30 * time.Second
	MaxLen   = 280
	BotName  = "cookbot"
)

type Store struct {
	db  *sql.DB
	Now func() time.Time
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, Now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ms() int64 { return s.Now().UnixMilli() }

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func randomCode(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func Clean(s string, max int) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || len([]rune(s)) > max {
		return "", ErrInvalid
	}
	return s, nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type User struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Rating float64 `json:"rating"`
	Tier   string  `json:"tier"`
	Wins   int     `json:"wins"`
	Losses int     `json:"losses"`
}

func (u *User) fill() { u.Tier = elo.TierOf(u.Rating).Name }

func (s *Store) CreateUser(ctx context.Context, name string) (User, string, error) {
	name, err := Clean(name, 20)
	if err != nil {
		return User{}, "", err
	}
	if strings.EqualFold(name, BotName) {
		return User{}, "", ErrInvalid
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (name, token_hash, created_at) VALUES (?, ?, ?)`, name, hashToken(token), s.ms())
	if err != nil {
		return User{}, "", err
	}
	id, _ := res.LastInsertId()
	u := User{ID: id, Name: name, Rating: elo.Start}
	u.fill()
	return u, token, nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Rating, &u.Wins, &u.Losses)
	u.fill()
	return u, notFound(err)
}

func (s *Store) UserByToken(ctx context.Context, token string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, name, rating, wins, losses FROM users WHERE token_hash = ?`, hashToken(token)))
}

func (s *Store) User(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, name, rating, wins, losses FROM users WHERE id = ?`, id))
}

func (s *Store) Leaderboard(ctx context.Context, limit int) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, rating, wins, losses FROM users WHERE wins + losses > 0
		ORDER BY rating DESC, wins DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Rating, &u.Wins, &u.Losses); err != nil {
			return nil, err
		}
		u.fill()
		out = append(out, u)
	}
	return out, rows.Err()
}
