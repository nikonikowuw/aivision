package camera

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeduceSubStream(t *testing.T) {
	tests := []struct {
		name        string
		mainURL     string
		expectedSub string
		brand       string
	}{
		{
			name:        "Hikvision ISAPI Channel 101",
			mainURL:     "rtsp://admin:pass123@192.168.1.64:554/Streaming/Channels/101",
			expectedSub: "rtsp://admin:pass123@192.168.1.64:554/Streaming/Channels/102",
			brand:       "Hikvision",
		},
		{
			name:        "Hikvision Old Stream",
			mainURL:     "rtsp://admin:pass123@192.168.1.64:554/h264/ch1/main/av_stream",
			expectedSub: "rtsp://admin:pass123@192.168.1.64:554/h264/ch1/sub/av_stream",
			brand:       "Hikvision",
		},
		{
			name:        "Dahua Channel 1 Subtype 0",
			mainURL:     "rtsp://admin:pass123@192.168.1.100:554/cam/realmonitor?channel=1&subtype=0",
			expectedSub: "rtsp://admin:pass123@192.168.1.100:554/cam/realmonitor?channel=1&subtype=1",
			brand:       "Dahua",
		},
		{
			name:        "Uniview Video1",
			mainURL:     "rtsp://admin:pass123@192.168.1.120:554/video1",
			expectedSub: "rtsp://admin:pass123@192.168.1.120:554/video2",
			brand:       "Uniview",
		},
		{
			name:        "Uniview Unicast Live",
			mainURL:     "rtsp://admin:pass123@192.168.1.120:554/unicast/c1/s0/live",
			expectedSub: "rtsp://admin:pass123@192.168.1.120:554/unicast/c1/s1/live",
			brand:       "Uniview",
		},
		{
			name:        "TP-Link Stream1",
			mainURL:     "rtsp://admin:pass123@192.168.1.150:554/stream1",
			expectedSub: "rtsp://admin:pass123@192.168.1.150:554/stream2",
			brand:       "TP-Link",
		},
		{
			name:        "Generic Main",
			mainURL:     "rtsp://admin:pass123@192.168.1.200:554/live/main",
			expectedSub: "rtsp://admin:pass123@192.168.1.200:554/live/sub",
			brand:       "Generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := DeduceSubStream(tt.mainURL)
			assert.NotEmpty(t, candidates)
			assert.Equal(t, tt.expectedSub, candidates[0].SubURL)
			assert.Equal(t, tt.brand, candidates[0].Brand)

			primary := DeducePrimarySubStream(tt.mainURL)
			assert.Equal(t, tt.expectedSub, primary)
		})
	}

	t.Run("Empty and Invalid URLs", func(t *testing.T) {
		assert.Empty(t, DeduceSubStream(""))
		assert.Empty(t, DeducePrimarySubStream(""))
		assert.Empty(t, DeducePrimarySubStream("::not a valid url::"))
	})
}
