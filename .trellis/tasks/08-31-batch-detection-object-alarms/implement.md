# Implementation Plan

## Phase A: Contract and fixtures

1. Update result-contract comments/spec to define one callback as one detection batch and one Engine `AlarmEvent` as one target alarm.
2. Add deterministic multi-object result fixture/helpers for Engine tests, including unique batch ID, four objects, and one full-frame image request.
3. Decide and encode shared-image ownership in Engine test doubles before changing production capture flow.

Verification: contract tests describe one callback -> N target alarms, unique IDs, one image encode.

## Phase B: Algorithm package

1. Refactor YOLOv8n normal result emission to collect cooldown-eligible objects and call `on_result` once with the complete vector.
2. Keep self-test exactly one callback and ensure normal empty batches produce zero callbacks.
3. Update package tests for multi-object serialization/emission behavior and runner expectations.

Verification: `make -C algo-packages/macos/arm64/yolov8n build`, `test`, and `run`; fixed image prints all four objects in one JSON.

## Phase C: Engine fan-out

1. Extract validated batch objects from the result JSON.
2. Generate per-target event IDs with collision checks and preserve per-target metadata.
3. Build one shared capture unit for the batch; retain/release the frame exactly once and encode the full frame once.
4. Enqueue/report one `AlarmEvent` per target with the shared image reference and preserve event-id deduplication.
5. Update image catalog/refcount/orphan reconciliation as required by the chosen shared-image ownership model.

Verification: Engine unit/integration fixture reports four one-object alarms, all share one image, duplicate IDs are ignored, and failure/shutdown releases resources.

## Phase D: Tests and docs

1. Update standalone runner capture/result tests.
2. Add tests for empty batches, malformed batch objects, duplicate target IDs, target cooldown, queue drops, image encode failure and multiple instances.
3. Update `.trellis/spec/engine` result/image guidance and task references.
4. Run formatting/static checks, package tests, Engine tests, sanitizer checks where available, then inspect the final diff for unrelated changes.

Verification: required commands and skipped checks are recorded in the final response.

## Rollback Points

- Before Engine capture changes: algorithm can be reverted to the prior per-object emission without changing ABI layout.
- Before contract/spec update: do not ship mixed producer/consumer semantics.
- Shared image support must remain behind one cohesive Engine change; do not leave per-event duplicate capture as the final implementation.
