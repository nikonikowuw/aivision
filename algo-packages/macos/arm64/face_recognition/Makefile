all: build

configure:
	cmake -B build -DCMAKE_BUILD_TYPE=Release

build:
	cmake --build build --config Release -j$$(sysctl -n hw.ncpu)

test: build
	ctest --test-dir build --output-on-failure

run: build
	./build/src/runner/face_recognition_runner run $(IMAGE)

extract: build
	./build/src/runner/face_recognition_runner extract $(IMAGE)

register: extract

selftest: build
	./build/src/runner/face_recognition_runner selftest

benchmark: build
	./build/src/runner/face_recognition_runner benchmark

asan:
	cmake -B build_asan -DCMAKE_BUILD_TYPE=Debug -DENABLE_ASAN=ON
	cmake --build build_asan --config Debug -j$$(sysctl -n hw.ncpu)
	ctest --test-dir build_asan --output-on-failure
	FACE_RECOGNITION_DYLIB=build_asan/libface_recognition.dylib ./build_asan/src/runner/face_recognition_runner run

package: build
	cmake --build build --target package_face_recognition

clean:
	rm -rf build build_asan lib result.jpg aligned_face*.jpg package.zip package.zip.sha256 *.zip *.zip.sha256

.PHONY: all configure build test run extract register selftest benchmark asan package clean
