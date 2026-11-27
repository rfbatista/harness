package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/ports"
)

var _ ports.PersonCredentials = (*PersonCredentialRepository)(nil)

// PersonCredentialModel is the hash of a person credential.
type PersonCredentialModel struct {
	Hash      string `gorm:"column:hash;primaryKey"`
	CreatedAt int64  `gorm:"column:created_at"`
}

func (PersonCredentialModel) TableName() string { return "person_credentials" }

type PersonCredentialRepository struct{ db *gorm.DB }

func NewPersonCredentialRepository(db *gorm.DB) *PersonCredentialRepository {
	return &PersonCredentialRepository{db: db}
}

func (r *PersonCredentialRepository) Add(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).Create(&PersonCredentialModel{Hash: hash, CreatedAt: time.Now().Unix()}).Error
}

func (r *PersonCredentialRepository) Has(ctx context.Context, hash string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&PersonCredentialModel{}).Where("hash = ?", hash).Count(&n).Error
	return n > 0, err
}

func (r *PersonCredentialRepository) Count(ctx context.Context) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&PersonCredentialModel{}).Count(&n).Error
	return int(n), err
}
