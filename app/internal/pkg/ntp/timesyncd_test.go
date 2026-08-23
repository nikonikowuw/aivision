//go:build linux

package ntp

import "testing"

func TestParseTimesyncdStatus(t *testing.T) {
	status := parseTimesyncdStatus("ServerName=ntp.aliyun.com\nOffset=+0.002s\n")
	if status.Source != "ntp.aliyun.com" {
		t.Fatalf("source = %q, want ntp.aliyun.com", status.Source)
	}
	if status.Offset != "+0.002s" {
		t.Fatalf("offset = %q, want +0.002s", status.Offset)
	}
	if status.Synced {
		t.Fatal("synced = true before synchronization flag is applied")
	}
}

func TestParseNTPsynchronized(t *testing.T) {
	for _, test := range []struct {
		output string
		want   bool
	}{
		{output: "yes\n", want: true},
		{output: "NTPSynchronized=no\n", want: false},
	} {
		if got := parseNTPsynchronized(test.output); got != test.want {
			t.Errorf("parseNTPsynchronized(%q) = %v, want %v", test.output, got, test.want)
		}
	}
}
