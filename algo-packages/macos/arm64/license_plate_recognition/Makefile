all: build

configure:
	cmake -B build -DCMAKE_BUILD_TYPE=Release

build: configure
	cmake --build build --config Release -j$$(sysctl -n hw.ncpu)

run: build
	./build/src/runner/license_plate_recognition_runner

benchmark: build
	./build/src/runner/license_plate_recognition_runner benchmark

asan:
	cmake -B build_asan -DCMAKE_BUILD_TYPE=Debug -DENABLE_ASAN=ON
	cmake --build build_asan --config Debug -j$$(sysctl -n hw.ncpu)
	ctest --test-dir build_asan --output-on-failure

package: build
	cmake --build build --target package_license_plate_recognition

clean:
	rm -rf build build_asan lib result.jpg package.zip package.zip.sha256 *.zip *.zip.sha256

.PHONY: all configure build run benchmark asan package clean
