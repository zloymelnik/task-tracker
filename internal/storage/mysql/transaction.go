package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type txKey struct{}

func getExecutor(ctx context.Context, db *sqlx.DB) Executor {
	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}
	return db
}

func NewTxManager(db *sqlx.DB) *TxManager {
	return &TxManager{db: db}
}

func (m *TxManager) Do(
	ctx context.Context,
	fn func(context.Context) error,
) (resultErr error) {
	if _, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return fmt.Errorf("MySQL: Transaction Manager: Transaction already exists")
	}
	tx, err := m.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("MySQL: Transaction Manager: Failed start transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr,
				fmt.Errorf("MySQL: Transaction Manager: Failed rollback: %w", err))
		}
	}()
	txCtx := context.WithValue(ctx, txKey{}, tx)
	err = fn(txCtx)
	if err != nil {
		return fmt.Errorf("MySQL: Transaction Manager: Failed request: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("MySQL: Transaction Manager: Failed commit: %w", err)
	}
	return nil
}
