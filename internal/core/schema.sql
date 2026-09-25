CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    token_hash TEXT    NOT NULL UNIQUE,
    rating     REAL    NOT NULL DEFAULT 1000,
    wins       INTEGER NOT NULL DEFAULT 0,
    losses     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS matches (
    id          INTEGER PRIMARY KEY,
    code        TEXT    NOT NULL UNIQUE,
    prompt      TEXT    NOT NULL,
    prompt_id   INTEGER NOT NULL DEFAULT 0,
    a_id        INTEGER REFERENCES users(id),
    a_bot       INTEGER NOT NULL DEFAULT 0,
    b_id        INTEGER REFERENCES users(id),
    b_bot       INTEGER NOT NULL DEFAULT 0,
    private     INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL,
    turn        INTEGER NOT NULL DEFAULT 0,
    deadline    INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    started_at  INTEGER,
    ended_at    INTEGER,
    winner      TEXT,
    decided_by  TEXT,
    score_a     INTEGER,
    crowd_a     INTEGER NOT NULL DEFAULT 0,
    crowd_b     INTEGER NOT NULL DEFAULT 0,
    headline    TEXT,
    reason      TEXT,
    roast_a     TEXT,
    roast_b     TEXT,
    delta       REAL
);

CREATE INDEX IF NOT EXISTS matches_status ON matches (status, deadline);

CREATE TABLE IF NOT EXISTS turns (
    match_id   INTEGER NOT NULL REFERENCES matches(id),
    idx        INTEGER NOT NULL,
    side       TEXT    NOT NULL,
    text       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (match_id, idx)
);

CREATE TABLE IF NOT EXISTS votes (
    match_id   INTEGER NOT NULL REFERENCES matches(id),
    user_id    INTEGER NOT NULL REFERENCES users(id),
    side       TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (match_id, user_id)
);

CREATE TABLE IF NOT EXISTS reports (
    id         INTEGER PRIMARY KEY,
    match_id   INTEGER NOT NULL REFERENCES matches(id),
    user_id    INTEGER NOT NULL REFERENCES users(id),
    reason     TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (match_id, user_id)
);
