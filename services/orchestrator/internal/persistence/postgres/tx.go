package postgres

import (
	"context"
	"fmt"

	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunInTx executes fn inside a serializable transaction.
// If fn returns an error the transaction is rolled back; otherwise it is committed.
func RunInTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, q sqlcgen.Querier) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err = fn(ctx, sqlcgen.New(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
