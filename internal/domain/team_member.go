package domain

import (
	"time"

	"github.com/google/uuid"
)

type TeamRole string

const (
	OwnerRole  TeamRole = "owner"
	AdminRole  TeamRole = "admin"
	MemberRole TeamRole = "member"
)

type TeamMember struct {
	TeamID   uuid.UUID
	UserID   uuid.UUID
	Role     TeamRole
	JoinedAt time.Time
}
