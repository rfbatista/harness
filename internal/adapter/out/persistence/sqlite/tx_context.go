package sqlite

import (
	"context"

	"gorm.io/gorm"
)

type txKey struct{}

// withTx makes ctx carry tx, so a store of the same database called inside a
// transaction joins it instead of waiting for the writer connection.
func withTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// dbFor is the transaction ctx carries, else db.
func dbFor(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}
