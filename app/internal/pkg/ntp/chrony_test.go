//go:build linux

package ntp

import "testing"

func TestParseChronyTracking(t *testing.T) {
	status := parseChronyTracking(`Reference ID    : CB00710F (ntp.aliyun.com)
Stratum         : 3
Ref time (UTC)  : Fri Aug 22 08:30:00 2025
Last offset     : +0.002345 seconds
Leap status     : Normal`)

	if !status.Synced {
		t.Fatal("synced = false, want true")
	}
	if status.Source != "ntp.aliyun.com" {
		t.Fatalf("source = %q, want ntp.aliyun.com", status.Source)
	}
	if status.Offset != "+0.002345s" {
		t.Fatalf("offset = %q, want +0.002345s", status.Offset)
	}
	if status.LastSyncTime == nil || *status.LastSyncTime != "2025-08-22T08:30:00Z" {
		t.Fatalf("last sync time = %v, want 2025-08-22T08:30:00Z", status.LastSyncTime)
	}
}

func TestParseChronyTrackingUnsynchronized(t *testing.T) {
	status := parseChronyTracking(`Reference ID    : 00000000 ()
Leap status     : Not synchronised`)
	if status.Synced {
		t.Fatal("synced = true, want false")
	}
	if status.Source != "" {
		t.Fatalf("source = %q, want empty", status.Source)
	}
}
