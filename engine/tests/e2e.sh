#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENGINE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_DIR="${ENGINE_DIR}/build"
TEST_TMP_DIR="${ENGINE_DIR}/build/e2e_tmp_$$"

echo "=== [E2E] Starting End-to-End Tests for Engine & Stub Server ==="

mkdir -p "${TEST_TMP_DIR}"
mkdir -p "${TEST_TMP_DIR}/var/packages"
mkdir -p "${TEST_TMP_DIR}/var/images"

APP_SOCK="${TEST_TMP_DIR}/app.sock"
ENGINE_SOCK="${TEST_TMP_DIR}/engine.sock"
HTTP_PORT="9199"
HTTP_URL="http://127.0.0.1:${HTTP_PORT}"
VALIDATOR_BIN="${BUILD_DIR}/package_validator"
ENGINE_BIN="${BUILD_DIR}/aivision-engine"
STUB_BIN="${TEST_TMP_DIR}/stub_server"
CLIENT_BIN="${TEST_TMP_DIR}/stub_client"

STUB_PID=""
ENGINE_PID=""

cleanup() {
    echo "=== [E2E] Cleaning up processes and temp files ==="
    if [[ -n "${ENGINE_PID}" ]] && kill -0 "${ENGINE_PID}" 2>/dev/null; then
        kill "${ENGINE_PID}" 2>/dev/null || true
        wait "${ENGINE_PID}" 2>/dev/null || true
    fi
    if [[ -n "${STUB_PID}" ]] && kill -0 "${STUB_PID}" 2>/dev/null; then
        kill "${STUB_PID}" 2>/dev/null || true
        wait "${STUB_PID}" 2>/dev/null || true
    fi
    rm -rf "${TEST_TMP_DIR}"
    echo "=== [E2E] Cleanup finished ==="
}
trap cleanup EXIT INT TERM

# 1. Compile Go stub and client
echo "--> Compiling stub_server and client..."
(cd "${ENGINE_DIR}/tests/stub_server" && go build -o "${STUB_BIN}" . && go build -o "${CLIENT_BIN}" ./cmd/client/main.go)

# 2. Start Go stub server
echo "--> Launching Stub Server (UDS: ${APP_SOCK}, HTTP: ${HTTP_PORT})..."
"${STUB_BIN}" -sock "${APP_SOCK}" -http "127.0.0.1:${HTTP_PORT}" &
STUB_PID=$!

# Wait for Stub Server HTTP healthz
for i in {1..30}; do
    if curl -s -f "${HTTP_URL}/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done
curl -s -f "${HTTP_URL}/healthz" >/dev/null 2>&1 || (echo "Stub server failed to start" && exit 1)
echo "    Stub Server is ready."

# 3. Start C++ Engine
echo "--> Launching Engine (Engine Sock: ${ENGINE_SOCK}, App Sock: ${APP_SOCK})..."
export AIVISION_ENGINE_SOCKET="${ENGINE_SOCK}"
export AIVISION_APP_SOCKET="${APP_SOCK}"
export AIVISION_PACKAGE_DIR="${TEST_TMP_DIR}/var/packages"
export AIVISION_IMAGE_DIR="${TEST_TMP_DIR}/var/images"
export AIVISION_PACKAGE_VALIDATOR_PATH="${VALIDATOR_BIN}"

"${ENGINE_BIN}" &
ENGINE_PID=$!

# Wait for Engine socket
for i in {1..30}; do
    if [[ -S "${ENGINE_SOCK}" ]]; then
        break
    fi
    sleep 0.1
done
[[ -S "${ENGINE_SOCK}" ]] || (echo "Engine socket failed to appear" && exit 1)
echo "    Engine is ready."

# 4. Test QueryProfile
echo "--> Test 1: QueryProfile RPC"
PROFILE_OUT=$("${CLIENT_BIN}" -engine-sock "${ENGINE_SOCK}" -cmd query_profile)
echo "    Profile: ${PROFILE_OUT}"
echo "${PROFILE_OUT}" | grep -q "macos-arm64-coreml" || echo "${PROFILE_OUT}" | grep -q "mock"

# 5. Test QueryMetrics
echo "--> Test 2: QueryMetrics RPC"
METRICS_OUT=$("${CLIENT_BIN}" -engine-sock "${ENGINE_SOCK}" -cmd query_metrics)
echo "    Metrics: ${METRICS_OUT}"
echo "${METRICS_OUT}" | grep -q "uptimeSeconds"

# 6. Test InstallPackage
echo "--> Test 3: Install Package from fixture staging"
FIXTURE_PKG="${BUILD_DIR}/tests/fixtures/packages/mock_pkg"
PKG_STAGING="${TEST_TMP_DIR}/mock_pkg_staged"
cp -r "${FIXTURE_PKG}" "${PKG_STAGING}"

# Patch manifest platform_id in staging to match running engine if needed
ENGINE_PLATFORM=$(echo "${PROFILE_OUT}" | sed -n 's/.*"platformId":"\([^"]*\)".*/\1/p')
if [[ -n "${ENGINE_PLATFORM}" ]]; then
  sed -i '' "s/\"platform_id\": \"[^\"]*\"/\"platform_id\": \"${ENGINE_PLATFORM}\"/g" "${PKG_STAGING}/manifest.json" 2>/dev/null || sed -i "s/\"platform_id\": \"[^\"]*\"/\"platform_id\": \"${ENGINE_PLATFORM}\"/g" "${PKG_STAGING}/manifest.json"
fi

cat <<EOF > "${TEST_TMP_DIR}/install_pkg.json"
{
  "packagePath": "${PKG_STAGING}"
}
EOF
INSTALL_OUT=$("${CLIENT_BIN}" -engine-sock "${ENGINE_SOCK}" -cmd install_package -payload "${TEST_TMP_DIR}/install_pkg.json")
echo "    InstallPackage result: ${INSTALL_OUT}"
echo "${INSTALL_OUT}" | grep -q 'algorithmId.*mock-detector'

# 7. Test ApplyDesiredState via Engine RPC
echo "--> Test 4: ApplyDesiredState via Engine RPC"
cat <<EOF > "${TEST_TMP_DIR}/desired_state_v1.json"
{
  "deviceId": "dev-001",
  "revision": "1",
  "tasks": [
    {
      "cameraId": "cam-001",
      "rtspUrl": "rtsp://127.0.0.1:8554/live/test",
      "enabled": true
    }
  ],
  "instances": [
    {
      "instanceId": "inst-001",
      "cameraId": "cam-001",
      "algorithmId": "mock-detector",
      "algorithmVersion": "1.0.0",
      "analysisFps": 1,
      "paramsJson": "{\"threshold\":0.5}",
      "enabled": true
    }
  ],
  "activePackageVersions": [
    {
      "algorithmId": "mock-detector",
      "version": "1.0.0"
    }
  ]
}
EOF
APPLY_OUT=$("${CLIENT_BIN}" -engine-sock "${ENGINE_SOCK}" -cmd apply_desired_state -payload "${TEST_TMP_DIR}/desired_state_v1.json")
echo "    ApplyDesiredState result: ${APPLY_OUT}"
echo "${APPLY_OUT}" | grep -q 'appliedRevision.*1'

# 8. Verify Task and Instance State reported to Go Stub Server
echo "--> Test 5: Verify Reported Task & Instance State in Stub Server"
for i in {1..30}; do
  TASK_STATES=$(curl -s "${HTTP_URL}/task_states")
  INST_STATES=$(curl -s "${HTTP_URL}/instance_states")
  if echo "${TASK_STATES}" | grep -q "cam-001" && echo "${INST_STATES}" | grep -q "inst-001"; then
    break
  fi
  sleep 0.2
done
echo "    Task States in Stub: ${TASK_STATES}"
echo "${TASK_STATES}" | grep -q "cam-001"

echo "    Instance States in Stub: ${INST_STATES}"
echo "${INST_STATES}" | grep -q "inst-001"

# 9. Test Engine Reconnect and Pull DesiredState from Go Stub Server
echo "--> Test 6: Go Stub Server DesiredState sync on restart / revision update"
cat <<EOF > "${TEST_TMP_DIR}/desired_state_v2.json"
{
  "deviceId": "dev-001",
  "revision": "2",
  "tasks": [
    {
      "cameraId": "cam-001",
      "rtspUrl": "rtsp://127.0.0.1:8554/live/test",
      "enabled": true
    },
    {
      "cameraId": "cam-002",
      "rtspUrl": "rtsp://127.0.0.1:8554/live/test2",
      "enabled": true
    }
  ],
  "instances": [],
  "activePackageVersions": [
    {
      "algorithmId": "mock-detector",
      "version": "1.0.0"
    }
  ]
}
EOF
# Push desired state v2 to stub server
curl -s -X POST "${HTTP_URL}/desired_state" --data @"${TEST_TMP_DIR}/desired_state_v2.json"
echo "    Pushed Revision 2 to Stub Server."

# Wait for Engine control plane loop to pull and apply revision 2
echo "    Waiting for Engine to pull and reconcile desired state revision 2..."
sleep 2.5
TASK_STATES_V2=$(curl -s "${HTTP_URL}/task_states")
echo "    Task States after sync: ${TASK_STATES_V2}"
echo "${TASK_STATES_V2}" | grep -q "cam-002"

# 10. Test Stub Server Restart and Engine Reconnect
echo "--> Test 7: Kill and Restart Go Stub Server"
kill -TERM "${STUB_PID}"
wait "${STUB_PID}" 2>/dev/null || true
STUB_PID=""

echo "    Stub Server killed. Restarting..."
"${STUB_BIN}" -sock "${APP_SOCK}" -http "127.0.0.1:${HTTP_PORT}" &
STUB_PID=$!
for i in {1..30}; do
    if curl -s -f "${HTTP_URL}/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

# Set Revision 3 on newly restarted Stub Server
cat <<EOF > "${TEST_TMP_DIR}/desired_state_v3.json"
{
  "deviceId": "dev-001",
  "revision": "3",
  "tasks": [
    {
      "cameraId": "cam-003",
      "rtspUrl": "rtsp://127.0.0.1:8554/live/test3",
      "enabled": true
    }
  ],
  "instances": [],
  "activePackageVersions": []
}
EOF
curl -s -X POST "${HTTP_URL}/desired_state" --data @"${TEST_TMP_DIR}/desired_state_v3.json"
echo "    Pushed Revision 3 to restarted Stub Server. Waiting for Engine sync..."
sleep 2.5

TASK_STATES_V3=$(curl -s "${HTTP_URL}/task_states")
echo "    Task States after reconnect: ${TASK_STATES_V3}"
echo "${TASK_STATES_V3}" | grep -q "cam-003"

echo "=== [E2E] All tests PASSED successfully! ==="
