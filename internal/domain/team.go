package domain

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	CreatedBy uuid.UUID
}

type TeamFilter struct {
	UserID *uuid.UUID
	Offset int
	Limit  int
}
