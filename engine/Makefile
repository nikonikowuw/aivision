.PHONY: all configure build test lint asan tsan clean clean-zlm-archives e2e \
        proto core media platform validator app

BUILD_DIR ?= build
GENERATOR ?= Ninja
ZLM_RELEASE_DIR ?= $(BUILD_DIR)/zlm_release

all: build

clean-zlm-archives:
	rm -f $(ZLM_RELEASE_DIR)/lib*.a
	rm -f $(BUILD_DIR)_asan/zlm_release/lib*.a
	rm -f $(BUILD_DIR)_tsan/zlm_release/lib*.a
	rm -f third_party/ZLMediaKit/release/*/*/lib*.a

configure: clean-zlm-archives
	cmake -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=Debug

$(BUILD_DIR)/CMakeCache.txt:
	@$(MAKE) configure

build: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR)

# Module-level targets
proto: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target proto_gen

core: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target engine_core

media: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target media_zlm

platform: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target platform_macos

validator: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target package_validator

app: $(BUILD_DIR)/CMakeCache.txt
	cmake --build $(BUILD_DIR) --target argus-engine

test:
	cd $(BUILD_DIR) && ./tests/unit_tests

lint:
	bash scripts/check-boundary.sh

asan: clean-zlm-archives
	@trap '$(MAKE) clean-zlm-archives' EXIT INT TERM; \
	cmake -B $(BUILD_DIR)_asan -G $(GENERATOR) -DENGINE_ENABLE_ASAN=ON -DCMAKE_BUILD_TYPE=Debug && \
	cmake --build $(BUILD_DIR)_asan && \
	(cd $(BUILD_DIR)_asan && ./tests/unit_tests)

tsan: clean-zlm-archives
	@trap '$(MAKE) clean-zlm-archives' EXIT INT TERM; \
	cmake -B $(BUILD_DIR)_tsan -G $(GENERATOR) -DENGINE_ENABLE_TSAN=ON -DCMAKE_BUILD_TYPE=Debug && \
	cmake --build $(BUILD_DIR)_tsan && \
	(cd $(BUILD_DIR)_tsan && ./tests/unit_tests)

clean:
	rm -rf $(BUILD_DIR) $(BUILD_DIR)_asan $(BUILD_DIR)_tsan
	rm -f tests/stub_server/stub_server tests/stub_server/stub_client
	$(MAKE) clean-zlm-archives

e2e: build
	bash tests/e2e.sh
