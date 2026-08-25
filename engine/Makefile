.PHONY: all configure build test lint asan tsan clean clean-zlm-archives e2e

BUILD_DIR ?= build
GENERATOR ?= Ninja
ZLM_RELEASE_DIR ?= $(BUILD_DIR)/zlm_release

all: build

clean-zlm-archives:
	rm -f $(ZLM_RELEASE_DIR)/lib*.a
	rm -f third_party/ZLMediaKit/release/*/*/lib*.a

configure:
	cmake -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=Debug

build: clean-zlm-archives
	cmake --build $(BUILD_DIR)

test:
	cd $(BUILD_DIR) && ctest --timeout 180 --output-on-failure -R unit_tests

lint:
	bash scripts/check-boundary.sh

asan: clean-zlm-archives
	cmake -B $(BUILD_DIR)_asan -G $(GENERATOR) -DENGINE_ENABLE_ASAN=ON -DCMAKE_BUILD_TYPE=Debug
	cmake --build $(BUILD_DIR)_asan
	cd $(BUILD_DIR)_asan && ctest --timeout 180 --output-on-failure -R unit_tests

tsan: clean-zlm-archives
	cmake -B $(BUILD_DIR)_tsan -G $(GENERATOR) -DENGINE_ENABLE_TSAN=ON -DCMAKE_BUILD_TYPE=Debug
	cmake --build $(BUILD_DIR)_tsan
	cd $(BUILD_DIR)_tsan && ctest --timeout 180 --output-on-failure -R unit_tests

clean:
	rm -rf $(BUILD_DIR) $(BUILD_DIR)_asan $(BUILD_DIR)_tsan
	rm -f tests/stub_server/stub_server tests/stub_server/stub_client
	$(MAKE) clean-zlm-archives

e2e: build
	bash tests/e2e.sh
