package core

import (
	"context"
	"database/sql"
	"time"

	"github.com/jkong7/cooked/internal/ai"
	"github.com/jkong7/cooked/internal/elo"
)

type Player struct {
	ID     *int64  `json:"id,omitempty"`
	Name   string  `json:"name"`
	Bot    bool    `json:"bot"`
	Rating float64 `json:"rating"`
	Tier   string  `json:"tier"`
}

type Turn struct {
	Idx  int       `json:"idx"`
	Side string    `json:"side"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type Match struct {
	ID        int64      `json:"-"`
	Code      string     `json:"code"`
	Prompt    string     `json:"prompt"`
	PromptID  int        `json:"prompt_id"`
	A         Player     `json:"a"`
	B         *Player    `json:"b,omitempty"`
	Private   bool       `json:"private"`
	Status    string     `json:"status"`
	Turn      int        `json:"turn"`
	Deadline  time.Time  `json:"deadline"`
	CreatedAt time.Time  `json:"created_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Turns     []Turn     `json:"turns"`
	CrowdA    int        `json:"crowd_a"`
	CrowdB    int        `json:"crowd_b"`
	Winner    string     `json:"winner,omitempty"`
	DecidedBy string     `json:"decided_by,omitempty"`
	ScoreA    int        `json:"score_a,omitempty"`
	Headline  string     `json:"headline,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	RoastA    string     `json:"roast_a,omitempty"`
	RoastB    string     `json:"roast_b,omitempty"`
	Delta     float64    `json:"delta,omitempty"`
}

func SideOfTurn(turn int) string {
	if turn%2 == 0 {
		return "a"
	}
	return "b"
}

func (m Match) Player(side string) *Player {
	if side == "a" {
		return &m.A
	}
	return m.B
}

func (m Match) SideOf(userID int64) string {
	if m.A.ID != nil && *m.A.ID == userID {
		return "a"
	}
	if m.B != nil && m.B.ID != nil && *m.B.ID == userID {
		return "b"
	}
	return ""
}

func (m Match) BotTurn() bool {
	if m.Status != "live" {
		return false
	}
	p := m.Player(SideOfTurn(m.Turn))
	return p != nil && p.Bot
}

func (m Match) Debate() ai.Debate {
	d := ai.Debate{Prompt: m.Prompt, NameA: m.A.Name, VotesA: m.CrowdA, VotesB: m.CrowdB}
	if m.B != nil {
		d.NameB = m.B.Name
	}
	for _, t := range m.Turns {
		d.Turns = append(d.Turns, ai.Turn{Side: t.Side, Text: t.Text})
	}
	return d
}

type Seat struct {
	UserID *int64
	Bot    bool
}

type NewMatch struct {
	Prompt   string
	PromptID int
	A        Seat
	B        *Seat
	Private  bool
}

func seatArgs(s *Seat) (any, int) {
	if s == nil {
		return nil, 0
	}
	bot := 0
	if s.Bot {
		bot = 1
	}
	if s.UserID == nil {
		return nil, bot
	}
	return *s.UserID, bot
}

func (s *Store) CreateMatch(ctx context.Context, in NewMatch) (Match, error) {
	prompt, err := Clean(in.Prompt, 140)
	if err != nil {
		return Match{}, err
	}
	status := "waiting"
	var deadline, started any = 0, nil
	if in.B != nil {
		status = "live"
		deadline, started = s.Now().Add(TurnTime).UnixMilli(), s.ms()
	}
	aID, aBot := seatArgs(&in.A)
	bID, bBot := seatArgs(in.B)
	private := 0
	if in.Private {
		private = 1
	}
	var code string
	for range 5 {
		code = randomCode(7)
		res, err := s.db.ExecContext(ctx, `INSERT INTO matches (code, prompt, prompt_id, a_id, a_bot, b_id, b_bot, private, status,
			deadline, created_at, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(code) DO NOTHING`,
			code, prompt, in.PromptID, aID, aBot, bID, bBot, private, status, deadline, s.ms(), started)
		if err != nil {
			return Match{}, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return s.Match(ctx, code)
		}
	}
	return Match{}, ErrInvalid
}

func (s *Store) Accept(ctx context.Context, code string, userID int64) (Match, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var status string
		var aID sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT status, a_id FROM matches WHERE code = ?`, code).Scan(&status, &aID); err != nil {
			return notFound(err)
		}
		if status != "waiting" {
			return ErrState
		}
		if aID.Valid && aID.Int64 == userID {
			return ErrForbidden
		}
		_, err := tx.ExecContext(ctx, `UPDATE matches SET b_id = ?, status = 'live', deadline = ?, started_at = ? WHERE code = ?`,
			userID, s.Now().Add(TurnTime).UnixMilli(), s.ms(), code)
		return err
	})
	if err != nil {
		return Match{}, err
	}
	return s.Match(ctx, code)
}

const matchCols = `m.id, m.code, m.prompt, m.prompt_id, m.a_id, m.a_bot, COALESCE(ua.name, ''), COALESCE(ua.rating, 1000),
	m.b_id, m.b_bot, COALESCE(ub.name, ''), COALESCE(ub.rating, 1000), m.private, m.status, m.turn, m.deadline, m.created_at,
	m.ended_at, COALESCE(m.winner, ''), COALESCE(m.decided_by, ''), COALESCE(m.score_a, 0), COALESCE(m.headline, ''),
	COALESCE(m.reason, ''), COALESCE(m.roast_a, ''), COALESCE(m.roast_b, ''), COALESCE(m.delta, 0), m.crowd_a, m.crowd_b`

const matchFrom = ` FROM matches m LEFT JOIN users ua ON ua.id = m.a_id LEFT JOIN users ub ON ub.id = m.b_id`

type scanner interface{ Scan(...any) error }

func player(id sql.NullInt64, bot bool, name string, rating float64) Player {
	p := Player{Name: name, Bot: bot, Rating: rating}
	if id.Valid {
		v := id.Int64
		p.ID = &v
	}
	if bot {
		p.Name, p.Rating = BotName, elo.Start
	}
	p.Tier = elo.TierOf(p.Rating).Name
	return p
}

func scanMatch(r scanner) (Match, error) {
	var m Match
	var aID, bID, ended sql.NullInt64
	var aBot, bBot, private bool
	var aName, bName string
	var aRating, bRating float64
	var deadline, created int64
	err := r.Scan(&m.ID, &m.Code, &m.Prompt, &m.PromptID, &aID, &aBot, &aName, &aRating, &bID, &bBot, &bName, &bRating,
		&private, &m.Status, &m.Turn, &deadline, &created, &ended, &m.Winner, &m.DecidedBy, &m.ScoreA, &m.Headline, &m.Reason,
		&m.RoastA, &m.RoastB, &m.Delta, &m.CrowdA, &m.CrowdB)
	if err != nil {
		return m, err
	}
	m.A = player(aID, aBot, aName, aRating)
	if bID.Valid || bBot {
		b := player(bID, bBot, bName, bRating)
		m.B = &b
	}
	m.Private = private
	m.Deadline, m.CreatedAt = time.UnixMilli(deadline), time.UnixMilli(created)
	if ended.Valid {
		t := time.UnixMilli(ended.Int64)
		m.EndedAt = &t
	}
	return m, nil
}

func (s *Store) loadTurns(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, m *Match) error {
	rows, err := q.QueryContext(ctx, `SELECT idx, side, text, created_at FROM turns WHERE match_id = ? ORDER BY idx`, m.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	m.Turns = []Turn{}
	for rows.Next() {
		var t Turn
		var at int64
		if err := rows.Scan(&t.Idx, &t.Side, &t.Text, &at); err != nil {
			return err
		}
		t.At = time.UnixMilli(at)
		m.Turns = append(m.Turns, t)
	}
	return rows.Err()
}

func (s *Store) countVotes(ctx context.Context, m *Match) error {
	if m.Status == "done" {
		return nil
	}
	return s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(side = 'a'), 0), COALESCE(sum(side = 'b'), 0) FROM votes WHERE match_id = ?`,
		m.ID).Scan(&m.CrowdA, &m.CrowdB)
}

func (s *Store) Match(ctx context.Context, code string) (Match, error) {
	m, err := scanMatch(s.db.QueryRowContext(ctx, `SELECT `+matchCols+matchFrom+` WHERE m.code = ?`, code))
	if err != nil {
		return m, notFound(err)
	}
	if err := s.loadTurns(ctx, s.db, &m); err != nil {
		return m, err
	}
	return m, s.countVotes(ctx, &m)
}

func (s *Store) Post(ctx context.Context, code string, side string, turn int, text string) (Match, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var id int64
		var status string
		var cur int
		if err := tx.QueryRowContext(ctx, `SELECT id, status, turn FROM matches WHERE code = ?`, code).Scan(&id, &status, &cur); err != nil {
			return notFound(err)
		}
		if status != "live" {
			return ErrState
		}
		if cur != turn || SideOfTurn(cur) != side {
			return ErrNotYours
		}
		now := s.Now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO turns (match_id, idx, side, text, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, cur, side, text, now.UnixMilli()); err != nil {
			return err
		}
		next, status, deadline := cur+1, "live", now.Add(TurnTime)
		if next >= Turns {
			status, deadline = "voting", now.Add(VoteTime)
		}
		_, err := tx.ExecContext(ctx, `UPDATE matches SET turn = ?, status = ?, deadline = ? WHERE id = ?`, next, status,
			deadline.UnixMilli(), id)
		return err
	})
	if err != nil {
		return Match{}, err
	}
	return s.Match(ctx, code)
}

func (s *Store) Vote(ctx context.Context, code string, userID int64, side string) (Match, error) {
	if side != "a" && side != "b" {
		return Match{}, ErrInvalid
	}
	m, err := s.Match(ctx, code)
	if err != nil {
		return m, err
	}
	if m.Status != "live" && m.Status != "voting" {
		return m, ErrState
	}
	if m.SideOf(userID) != "" {
		return m, ErrForbidden
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO votes (match_id, user_id, side, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(match_id, user_id) DO UPDATE SET side = excluded.side, updated_at = excluded.updated_at`,
		m.ID, userID, side, s.ms()); err != nil {
		return m, err
	}
	return s.Match(ctx, code)
}

type Due struct {
	Code   string
	Status string
	Turn   int
}

func (s *Store) Due(ctx context.Context) ([]Due, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code, status, turn FROM matches WHERE status IN ('live', 'voting') AND deadline <= ?
		ORDER BY deadline LIMIT 100`, s.ms())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Due
	for rows.Next() {
		var d Due
		if err := rows.Scan(&d.Code, &d.Status, &d.Turn); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) BeginJudging(ctx context.Context, code string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET status = 'judging' WHERE code = ? AND status = 'voting'`, code)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) Judging(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code FROM matches WHERE status = 'judging'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const CrowdQuorum = 3

func Decide(v ai.Verdict, crowdA, crowdB int) (winner, by string) {
	if crowdA+crowdB >= CrowdQuorum && crowdA != crowdB {
		if crowdA > crowdB {
			return "a", "crowd"
		}
		return "b", "crowd"
	}
	return v.Winner, "judge"
}

func (s *Store) Finish(ctx context.Context, code string, v ai.Verdict) (Match, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		m, err := scanMatch(tx.QueryRowContext(ctx, `SELECT `+matchCols+matchFrom+` WHERE m.code = ?`, code))
		if err != nil {
			return notFound(err)
		}
		if m.Status != "judging" {
			return ErrState
		}
		var ca, cb int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(side = 'a'), 0), COALESCE(sum(side = 'b'), 0) FROM votes
			WHERE match_id = ?`, m.ID).Scan(&ca, &cb); err != nil {
			return err
		}
		winner, by := Decide(v, ca, cb)
		win, lose := m.A, *m.B
		if winner == "b" {
			win, lose = *m.B, m.A
		}
		k := elo.K
		if win.Bot || lose.Bot {
			k = elo.BotK
		}
		newWin, newLose := elo.Update(win.Rating, lose.Rating, k)
		delta := newWin - win.Rating
		for _, p := range []struct {
			pl     Player
			rating float64
			won    bool
		}{{win, newWin, true}, {lose, newLose, false}} {
			if p.pl.Bot || p.pl.ID == nil {
				continue
			}
			col := "losses"
			if p.won {
				col = "wins"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET rating = ?, `+col+` = `+col+` + 1 WHERE id = ?`,
				p.rating, *p.pl.ID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE matches SET status = 'done', winner = ?, decided_by = ?, score_a = ?, headline = ?,
			reason = ?, roast_a = ?, roast_b = ?, crowd_a = ?, crowd_b = ?, delta = ?, ended_at = ? WHERE id = ?`,
			winner, by, v.ScoreA, v.Headline, v.Reason, v.RoastA, v.RoastB, ca, cb, delta, s.ms(), m.ID)
		return err
	})
	if err != nil {
		return Match{}, err
	}
	return s.Match(ctx, code)
}

func (s *Store) list(ctx context.Context, where string, limit int, args ...any) ([]Match, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+matchCols+matchFrom+` WHERE `+where+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	var out []Match
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.countVotes(ctx, &out[i]); err != nil {
			return nil, err
		}
		out[i].Turns = []Turn{}
	}
	if out == nil {
		out = []Match{}
	}
	return out, nil
}

func (s *Store) Live(ctx context.Context, limit int) ([]Match, error) {
	return s.list(ctx, `m.private = 0 AND m.status IN ('live', 'voting', 'judging') ORDER BY m.started_at DESC`, limit)
}

func (s *Store) Recent(ctx context.Context, limit int) ([]Match, error) {
	return s.list(ctx, `m.private = 0 AND m.status = 'done' ORDER BY m.ended_at DESC`, limit)
}

func (s *Store) History(ctx context.Context, userID int64, limit int) ([]Match, error) {
	return s.list(ctx, `(m.a_id = ? OR m.b_id = ?) AND m.status = 'done' ORDER BY m.ended_at DESC`, limit, userID, userID)
}

func (s *Store) Report(ctx context.Context, code string, userID int64, reason string) error {
	reason, err := Clean(reason, 200)
	if err != nil {
		return err
	}
	m, err := s.Match(ctx, code)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO reports (match_id, user_id, reason, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, m.ID, userID, reason, s.ms())
	return err
}

func (s *Store) ExpireWaiting(ctx context.Context, olderThan time.Duration) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE matches SET status = 'expired' WHERE status = 'waiting' AND created_at < ?`,
		s.Now().Add(-olderThan).UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
