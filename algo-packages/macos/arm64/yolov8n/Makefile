.PHONY: all configure build test run benchmark asan package clean

BUILD_DIR ?= build
GENERATOR ?= Ninja

all: build

configure:
	cmake -B $(BUILD_DIR) -G $(GENERATOR) -DCMAKE_BUILD_TYPE=Release

build: configure
	cmake --build $(BUILD_DIR)

test: build
	ctest --test-dir $(BUILD_DIR) --output-on-failure

run: build
	./$(BUILD_DIR)/run_local

benchmark: build
	./$(BUILD_DIR)/run_local --benchmark

asan:
	cmake -B $(BUILD_DIR)_asan -G $(GENERATOR) -DCMAKE_BUILD_TYPE=Debug -DCMAKE_C_FLAGS="-fsanitize=address,undefined -fno-omit-frame-pointer" -DCMAKE_CXX_FLAGS="-fsanitize=address,undefined -fno-omit-frame-pointer" -DCMAKE_OBJCXX_FLAGS="-fsanitize=address,undefined -fno-omit-frame-pointer" -DCMAKE_EXE_LINKER_FLAGS="-fsanitize=address,undefined" -DCMAKE_SHARED_LINKER_FLAGS="-fsanitize=address,undefined"
	cmake --build $(BUILD_DIR)_asan
	ctest --test-dir $(BUILD_DIR)_asan --output-on-failure
	./$(BUILD_DIR)_asan/run_local

package: build
	cmake --build $(BUILD_DIR) --target package_yolov8n

clean:
	rm -rf $(BUILD_DIR) $(BUILD_DIR)_asan
