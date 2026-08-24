#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURE_DIR="${1:-${SCRIPT_DIR}/fixtures/media}"
mkdir -p "${FIXTURE_DIR}"

H264_MP4="${FIXTURE_DIR}/test_1080p_h264.mp4"
H265_MP4="${FIXTURE_DIR}/test_1080p_h265.mp4"

# Generate deterministic 5-second 1080p 25fps H.264 video with color bars and timecode
if [[ ! -f "${H264_MP4}" ]]; then
  echo "Generating H.264 test video..."
  ffmpeg -y -f lavfi -i testsrc=duration=5:size=1920x1080:rate=25 \
    -c:v libx264 -profile:v high -level:v 4.1 -pix_fmt yuv420p \
    -g 25 -keyint_min 25 -x264-params "colorprim=bt709:transfer=bt709:colormatrix=bt709" \
    "${H264_MP4}" >/dev/null 2>&1
fi

# Generate deterministic 5-second 1080p 25fps H.265 (HEVC) video
if [[ ! -f "${H265_MP4}" ]]; then
  echo "Generating H.265 test video..."
  ffmpeg -y -f lavfi -i testsrc=duration=5:size=1920x1080:rate=25 \
    -c:v libx265 -pix_fmt yuv420p \
    -g 25 -keyint_min 25 -x265-params "colorprim=bt709:transfer=bt709:colormatrix=bt709" \
    "${H265_MP4}" >/dev/null 2>&1
fi

echo "Media fixtures generated at ${FIXTURE_DIR}"
