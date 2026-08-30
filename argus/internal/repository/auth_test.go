package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"argus/app/internal/model"
)

func TestAuthRepositoryRotateRefreshTokenRollsBackOnCreateFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.RefreshToken{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	expiresAt := time.Now().Add(time.Hour)
	oldToken := model.RefreshToken{UserID: 1, Token: "old-token", ExpiresAt: expiresAt}
	conflictingToken := model.RefreshToken{UserID: 1, Token: "new-token", ExpiresAt: expiresAt}
	if err := db.Create(&oldToken).Error; err != nil {
		t.Fatalf("create old token: %v", err)
	}
	if err := db.Create(&conflictingToken).Error; err != nil {
		t.Fatalf("create conflicting token: %v", err)
	}

	consumed, err := NewAuthRepository(db).RotateRefreshToken(context.Background(), oldToken.Token, &model.RefreshToken{
		UserID:    1,
		Token:     conflictingToken.Token,
		ExpiresAt: expiresAt,
	})
	if err == nil {
		t.Fatal("RotateRefreshToken should fail when new token conflicts")
	}
	if consumed {
		t.Fatal("failed rotation should not report the old token as consumed")
	}

	var persisted model.RefreshToken
	if err := db.First(&persisted, oldToken.ID).Error; err != nil {
		t.Fatalf("find old token: %v", err)
	}
	if persisted.Revoked {
		t.Fatal("old token should remain active after failed rotation")
	}
}
