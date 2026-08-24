.PHONY: all configure build test lint asan tsan clean clean-zlm-archives

BUILD_DIR ?= build
GENERATOR ?= Ninja
ZLM_RELEASE_DIR ?= $(BUILD_DIR)/zlm_release

all: build

clean-zlm-archives:
	rm -f $(ZLM_RELEASE_DIR)/lib*.a

configure:
	cmake -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=Debug

build: clean-zlm-archives
	cmake --build $(BUILD_DIR)

test:
	ctest --test-dir $(BUILD_DIR) --output-on-failure

lint:
	bash scripts/check-boundary.sh

asan: clean-zlm-archives
	cmake -B $(BUILD_DIR)_asan -G $(GENERATOR) -DENGINE_ENABLE_ASAN=ON -DCMAKE_BUILD_TYPE=Debug
	cmake --build $(BUILD_DIR)_asan
	ctest --test-dir $(BUILD_DIR)_asan --output-on-failure

tsan: clean-zlm-archives
	cmake -B $(BUILD_DIR)_tsan -G $(GENERATOR) -DENGINE_ENABLE_TSAN=ON -DCMAKE_BUILD_TYPE=Debug
	cmake --build $(BUILD_DIR)_tsan
	ctest --test-dir $(BUILD_DIR)_tsan --output-on-failure

clean:
	rm -rf $(BUILD_DIR) $(BUILD_DIR)_asan $(BUILD_DIR)_tsan
	$(MAKE) clean-zlm-archives
