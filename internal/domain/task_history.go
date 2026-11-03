package domain

import (
	"time"

	"github.com/google/uuid"
)

type FieldChange struct {
	Old any `json:"old"`
	New any `json:"new"`
}

type TaskChanges map[string]FieldChange

type TaskHistory struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	ChangedBy uuid.UUID
	Changes   TaskChanges
	CreatedAt time.Time
}
