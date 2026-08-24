# Minimal Makefile for RK3576 YOLOv8n Package

BUILD_DIR ?= build

.PHONY: all configure build test run benchmark asan package clean

all: build

configure:
	cmake -B $(BUILD_DIR) -DCMAKE_BUILD_TYPE=RelWithDebInfo

build: configure
	cmake --build $(BUILD_DIR) -j

test: build
	ctest --test-dir $(BUILD_DIR) --output-on-failure

run: build
	./$(BUILD_DIR)/run_local

benchmark: build
	./$(BUILD_DIR)/run_local --benchmark

asan:
	cmake -B $(BUILD_DIR)_asan -DCMAKE_BUILD_TYPE=Debug -DENABLE_ASAN=ON
	cmake --build $(BUILD_DIR)_asan -j
	ctest --test-dir $(BUILD_DIR)_asan --output-on-failure
	./$(BUILD_DIR)_asan/run_local

package: build
	cmake --build $(BUILD_DIR) --target package_yolov8n

clean:
	rm -rf $(BUILD_DIR) $(BUILD_DIR)_asan
