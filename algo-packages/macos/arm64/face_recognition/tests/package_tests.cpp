#include "postprocess/postprocessor.hpp"
#include "core/config.hpp"
#include <cassert>
#include <iostream>
#include <cmath>

using namespace face_recognition;

void test_embedding_normalization_and_encoding() {
    std::vector<float> raw(512, 1.0f);
    std::string b64;
    std::string err;

    assert(Postprocessor::process_and_encode_embedding(raw, b64, err));
    assert(!b64.empty());
    assert(err.empty());

    // Test with NaN
    raw[10] = NAN;
    assert(!Postprocessor::process_and_encode_embedding(raw, b64, err));
    assert(!err.empty());

    // Test with all zeros
    std::vector<float> zeros(512, 0.0f);
    assert(!Postprocessor::process_and_encode_embedding(zeros, b64, err));

    // Test with invalid size
    std::vector<float> short_vec(256, 1.0f);
    assert(!Postprocessor::process_and_encode_embedding(short_vec, b64, err));

    std::cout << "[PASS] test_embedding_normalization_and_encoding" << std::endl;
}

void test_json_serialization() {
    std::vector<RecognizedPerson> persons;

    RecognizedPerson p1{};
    p1.track_id = 1;
    p1.person_bbox[0] = 0.1f; p1.person_bbox[1] = 0.2f; p1.person_bbox[2] = 0.3f; p1.person_bbox[3] = 0.4f;
    p1.person_confidence = 0.95f;
    p1.has_face = true;
    p1.face_bbox[0] = 0.15f; p1.face_bbox[1] = 0.22f; p1.face_bbox[2] = 0.1f; p1.face_bbox[3] = 0.15f;
    p1.face_confidence = 0.88f;
    for (int i = 0; i < 10; ++i) p1.face_landmarks[i] = 0.2f + i * 0.01f;
    p1.embedding_base64 = "AAAA";

    RecognizedPerson p2{};
    p2.track_id = 2;
    p2.person_bbox[0] = 0.5f; p2.person_bbox[1] = 0.5f; p2.person_bbox[2] = 0.2f; p2.person_bbox[3] = 0.3f;
    p2.person_confidence = 0.90f;
    p2.has_face = false;

    persons.push_back(p1);
    persons.push_back(p2);

    std::string json = Postprocessor::serialize_recognition_json(persons, 42, 1700000000);
    assert(json.find("\"schema_version\": 1") != std::string::npos);
    assert(json.find("\"frame_id\": 42") != std::string::npos);
    assert(json.find("\"pts_ns\": 1700000000") != std::string::npos);
    assert(json.find("\"algorithm_type\": \"face_recognition\"") != std::string::npos);
    assert(json.find("\"track_id\": 1") != std::string::npos);
    assert(json.find("\"model\": \"glintr100\"") != std::string::npos);
    assert(json.find("\"face\": null") != std::string::npos);

    std::cout << "[PASS] test_json_serialization" << std::endl;
}

void test_config_parsing() {
    InstanceConfig base;
    assert(!base.enable_person_detection);
    assert(base.max_recognitions_per_track == 3);

    std::string json = R"({
        "enable_person_detection": true,
        "face_detection_threshold": 0.65,
        "max_recognitions_per_track": 5,
        "feature_mode": "all",
        "track_confirm_frames": 3
    })";

    InstanceConfig parsed = InstanceConfig::parse_from_json(json, base);
    assert(parsed.enable_person_detection == true);
    assert(std::abs(parsed.face_detection_threshold - 0.65f) < 1e-4f);
    assert(parsed.max_recognitions_per_track == 5);
    assert(parsed.feature_mode == "all");
    assert(parsed.track_confirm_frames == 3);

    std::cout << "[PASS] test_config_parsing" << std::endl;
}

int main() {
    test_embedding_normalization_and_encoding();
    test_json_serialization();
    test_config_parsing();
    std::cout << "All postprocess tests passed!" << std::endl;
    return 0;
}
