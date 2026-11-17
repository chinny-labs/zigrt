package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NewID returns a UUIDv7, so IDs sort by creation time.
func NewID() string { return uuid.Must(uuid.NewV7()).String() }

// Household is the single install-wide row (one install = one family).
type Household struct {
	ID        string
	Name      string
	Timezone  string
	CreatedAt time.Time
}

// Location returns the household's time zone, falling back to UTC.
func (h Household) Location() *time.Location {
	if loc, err := time.LoadLocation(h.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

type Role string

const (
	RoleParent  Role = "parent"
	RoleStudent Role = "student"
)

// User is a parent (email and password) or a student (PIN, no email).
type User struct {
	ID           string
	Role         Role
	DisplayName  string
	Avatar       string // a tone name the UI maps to a colour
	Email        string // parents only
	PasswordHash string // parents only
	PINHash      string // students only
	CreatedAt    time.Time
}

func (u User) IsParent() bool  { return u.Role == RoleParent }
func (u User) IsStudent() bool { return u.Role == RoleStudent }

// Initial is the first letter of the display name, for avatar tiles.
func (u User) Initial() string {
	for _, r := range u.DisplayName {
		return string(r)
	}
	return "?"
}

type Session struct {
	IDHash      string
	UserID      string
	DeviceLabel string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

// AuthAttempts tracks failed sign-ins per account (plan §2).
type AuthAttempts struct {
	UserID        string
	Failures      int
	WindowStart   *time.Time
	NextAllowedAt *time.Time
	LockedAt      *time.Time
}

// StudentSettings is what a parent chooses for one student (plan §8.1).
type StudentSettings struct {
	UserID         string
	CourseID       string // "" until a course is assigned
	DailyXPGoal    int
	RestDays       string // weekday numbers, 0 = Sunday; "06" = weekends
	FluencyEnabled bool
}

// DefaultDailyXPGoal is the goal a new student starts with (plan §3.7).
const DefaultDailyXPGoal = 15

type AuditEntry struct {
	ID        string
	ActorID   string
	Action    string
	TargetID  string
	Detail    json.RawMessage
	CreatedAt time.Time
}

// DrillKind says whether a set is answered on screen or printed.
type DrillKind string

const (
	KindDrill     DrillKind = "drill"
	KindWorksheet DrillKind = "worksheet"
)

// Drill is a saved problem set (plan §5). Settings and Problems are JSON the
// service owns; the store keeps them as written.
type Drill struct {
	ID        string
	OwnerID   string
	Kind      DrillKind
	Title     string
	Settings  json.RawMessage
	Seed      uint64
	Problems  json.RawMessage
	CreatedAt time.Time
}

// DrillResult is how one on-screen drill went.
type DrillResult struct {
	DrillID    string
	StudentID  string
	Correct    int
	Total      int
	DurationMS int64
	Answers    json.RawMessage
	FinishedAt time.Time
}
