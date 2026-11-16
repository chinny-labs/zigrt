// Package store is the only package with SQL in it (plan §7.3). Store is
// defined in domain terms, not tables; handlers and services only see this
// interface. The contract suite in storetest is the real definition of the
// behaviour both engines must share.
package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a unique constraint (a parent's email) is hit.
	ErrConflict = errors.New("already exists")
)

// Store is a Zigrt database.
type Store interface {
	Reader

	// Tx runs fn in one write transaction. When lockUser is non-empty, writes
	// for that user are serialized for the life of the transaction (SQLite:
	// BEGIN IMMEDIATE on a single-connection write pool; Postgres:
	// SELECT … FOR UPDATE on the users row), so check-then-write is safe.
	Tx(ctx context.Context, lockUser string, fn func(Tx) error) error

	Dialect() string // "sqlite" or "postgres"
	Ping(ctx context.Context) error
	Close() error
}

// Tx is the read/write view inside a transaction.
type Tx interface {
	Reader
	Writer
}

// Reader holds every query.
type Reader interface {
	GetHousehold(ctx context.Context) (Household, error)
	GetSetting(ctx context.Context, key string) (string, error)

	CountParents(ctx context.Context) (int, error)
	GetUser(ctx context.Context, id string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	// ListUsers lists one role, or everyone when role is "", by name.
	ListUsers(ctx context.Context, role Role) ([]User, error)

	GetSession(ctx context.Context, idHash string) (Session, error)
	// GetAuthAttempts returns a zero record when the user has no failures.
	GetAuthAttempts(ctx context.Context, userID string) (AuthAttempts, error)

	GetStudentSettings(ctx context.Context, userID string) (StudentSettings, error)

	GetDrill(ctx context.Context, id string) (Drill, error)
	// ListDrills lists an owner's sets, newest first.
	ListDrills(ctx context.Context, ownerID string, limit int) ([]Drill, error)
	GetDrillResult(ctx context.Context, drillID string) (DrillResult, error)

	// ListAudit lists entries newest first; targetID "" matches every target.
	ListAudit(ctx context.Context, targetID string, limit int) ([]AuditEntry, error)
}

// Writer holds every write.
type Writer interface {
	CreateHousehold(ctx context.Context, h Household) error
	UpdateHousehold(ctx context.Context, h Household) error
	PutSetting(ctx context.Context, key, value string, secret bool) error

	CreateUser(ctx context.Context, u User) error
	UpdateUser(ctx context.Context, u User) error
	// DeleteUser removes a user and everything that hangs off them.
	DeleteUser(ctx context.Context, id string) error

	CreateSession(ctx context.Context, s Session) error
	TouchSession(ctx context.Context, idHash string, lastSeen, expires time.Time) error
	RevokeSession(ctx context.Context, idHash string, at time.Time) error
	RevokeUserSessions(ctx context.Context, userID string, at time.Time) error
	// DeleteDeadSessions removes sessions that expired or were revoked before
	// cutoff, and reports how many.
	DeleteDeadSessions(ctx context.Context, cutoff time.Time) (int64, error)
	PutAuthAttempts(ctx context.Context, a AuthAttempts) error
	ClearAuthAttempts(ctx context.Context, userID string) error

	PutStudentSettings(ctx context.Context, s StudentSettings) error

	CreateDrill(ctx context.Context, d Drill) error
	// InsertDrillResult reports false, with no error, when the drill already
	// has a result: the first one wins.
	InsertDrillResult(ctx context.Context, r DrillResult) (bool, error)

	InsertAudit(ctx context.Context, a AuditEntry) error
}
