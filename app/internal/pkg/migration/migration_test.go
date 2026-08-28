package migration

import (
	"testing"

	"niko-vue-admin/app/migrations"
)

func TestCalculateLatestVersion(t *testing.T) {
	ver, err := calculateLatestVersion(migrations.FS)
	if err != nil {
		t.Fatalf("calculateLatestVersion: %v", err)
	}
	if ver != 19 {
		t.Errorf("latest version = %d, want 19", ver)
	}
}
