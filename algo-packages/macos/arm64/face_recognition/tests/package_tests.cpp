#include "postprocess/postprocessor.hpp"
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

    std::string json = Postprocessor::serialize_recognition_json(persons);
    assert(json.find("\"schema_version\": 1") != std::string::npos);
    assert(json.find("\"track_id\": 1") != std::string::npos);
    assert(json.find("\"model\": \"glintr100\"") != std::string::npos);
    assert(json.find("\"face\": null") != std::string::npos);

    std::cout << "[PASS] test_json_serialization" << std::endl;
}

int main() {
    test_embedding_normalization_and_encoding();
    test_json_serialization();
    std::cout << "All postprocess tests passed!" << std::endl;
    return 0;
}
