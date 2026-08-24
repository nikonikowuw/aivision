#pragma once

#include <cstdlib>
#include <fstream>
#include <sstream>
#include <string>
#include <unordered_map>

namespace aivision::utils {

class EnvReader {
public:
    static std::unordered_map<std::string, std::string> load_file(const std::string& path) {
        std::unordered_map<std::string, std::string> env;
        std::ifstream file(path);
        if (!file.is_open()) return env;

        std::string line;
        while (std::getline(file, line)) {
            if (line.empty() || line[0] == '#') continue;
            size_t eq_pos = line.find('=');
            if (eq_pos == std::string::npos) continue;
            std::string key = line.substr(0, eq_pos);
            std::string val = line.substr(eq_pos + 1);
            while (!key.empty() && (key.back() == ' ' || key.back() == '\t' || key.back() == '\r')) key.pop_back();
            while (!key.empty() && (key.front() == ' ' || key.front() == '\t')) key.erase(key.begin());
            while (!val.empty() && (val.front() == ' ' || val.front() == '\t')) val.erase(val.begin());
            while (!val.empty() && (val.back() == ' ' || val.back() == '\t' || val.back() == '\r')) val.pop_back();
            if (!val.empty() && (val.front() == '"' || val.front() == '\'')) val.erase(val.begin());
            if (!val.empty() && (val.back() == '"' || val.back() == '\'')) val.pop_back();
            env[key] = val;
        }
        return env;
    }

    static std::string get(const std::string& key, const std::string& default_val = "",
                           const std::unordered_map<std::string, std::string>& local_env = {}) {
        const char* sys = std::getenv(key.c_str());
        if (sys && *sys != '\0') return std::string(sys);
        const auto it = local_env.find(key);
        return it != local_env.end() ? it->second : default_val;
    }

    static float get_float(const std::string& key, float default_val,
                           const std::unordered_map<std::string, std::string>& local_env = {}) {
        const std::string value = get(key, "", local_env);
        if (value.empty()) return default_val;
        try {
            return std::stof(value);
        } catch (...) {
            return default_val;
        }
    }

    static int get_int(const std::string& key, int default_val,
                       const std::unordered_map<std::string, std::string>& local_env = {}) {
        const std::string value = get(key, "", local_env);
        if (value.empty()) return default_val;
        try {
            size_t parsed = 0;
            const int result = std::stoi(value, &parsed);
            return parsed == value.size() ? result : default_val;
        } catch (...) {
            return default_val;
        }
    }
};

} // namespace aivision::utils
