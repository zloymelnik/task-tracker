package mysql

import (
	"database/sql"
	"errors"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func uuidToBytes(uuid uuid.UUID) []byte {
	b := make([]byte, 16)
	copy(b, uuid[:])
	return b
}

func bytesToUUID(b []byte) (uuid.UUID, error) {
	return uuid.FromBytes(b)
}

func isErrEmailAlreadyExists(err error) bool {
	var e *mysqldriver.MySQLError
	if !errors.As(err, &e) || e.Number != 1062 {
		return false
	}

	return strings.HasSuffix(e.Message, "for key 'users.uq_users_email'") ||
		strings.HasSuffix(e.Message, "for key 'uq_users_email'")
}

func isErrUserNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
