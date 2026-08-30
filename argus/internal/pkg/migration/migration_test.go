package migration

import (
	"testing"

	"argus/app/migrations"
)

func TestCalculateLatestVersion(t *testing.T) {
	ver, err := calculateLatestVersion(migrations.FS)
	if err != nil {
		t.Fatalf("calculateLatestVersion: %v", err)
	}
	if ver != 22 {
		t.Errorf("latest version = %d, want 22", ver)
	}
}
