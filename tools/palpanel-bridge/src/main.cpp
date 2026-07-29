#include <Mod/CppUserModBase.hpp>

#include <WinSock2.h>
#include <WS2tcpip.h>
#include <Windows.h>

#include <atomic>
#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <map>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>

extern "C" IMAGE_DOS_HEADER __ImageBase;

namespace
{
struct Config
{
    std::string listen{"127.0.0.1"};
    unsigned short port{18083};
    std::string token{};
};

struct Job
{
    std::string id;
    std::string status{"queued"};
    bool unreal_initialized{};
    bool game_thread_tick_seen{};
};

std::filesystem::path mod_directory()
{
    std::wstring path(32768, L'\0');
    const auto length = GetModuleFileNameW(
        reinterpret_cast<HMODULE>(&__ImageBase), path.data(), static_cast<DWORD>(path.size()));
    if (length == 0 || length >= path.size()) return {};
    path.resize(length);
    return std::filesystem::path(path).parent_path().parent_path();
}

void append_log(const std::string& message)
{
    const auto directory = mod_directory();
    if (directory.empty()) return;
    std::ofstream output(directory / "PalPanelBridge.log", std::ios::app);
    if (output) output << message << '\n';
}

std::string trim(std::string value)
{
    const auto begin = value.find_first_not_of(" \t\r\n");
    if (begin == std::string::npos) return {};
    const auto end = value.find_last_not_of(" \t\r\n");
    return value.substr(begin, end - begin + 1);
}

Config load_config()
{
    Config config;
    if (const char* token = std::getenv("PALPANEL_BRIDGE_TOKEN"); token && *token) {
        config.token = token;
    }
    const auto config_path = mod_directory() / "config.ini";
    std::ifstream input(config_path);
    std::string line;
    while (std::getline(input, line)) {
        line = trim(line);
        if (line.empty() || line.front() == '#') continue;
        const auto separator = line.find('=');
        if (separator == std::string::npos) continue;
        const auto key = trim(line.substr(0, separator));
        const auto value = trim(line.substr(separator + 1));
        if (key == "listen" && value == "127.0.0.1") config.listen = value;
        if (key == "token") config.token = value;
        if (key == "port") {
            try {
                const auto port = std::stoul(value);
                if (port > 0 && port <= 65535) config.port = static_cast<unsigned short>(port);
            } catch (...) {
            }
        }
    }
    if (config.token == "REPLACE_WITH_A_RANDOM_TOKEN") config.token.clear();
    append_log("config=" + config_path.string() + " port=" + std::to_string(config.port) +
               " token_configured=" + (config.token.empty() ? "false" : "true"));
    return config;
}

std::string response(int status, const std::string& body)
{
    const char* reason = status == 200 ? "OK" : status == 202 ? "Accepted" : status == 401 ? "Unauthorized"
                                                                                           : status == 404 ? "Not Found"
                                                                                                           : "Service Unavailable";
    std::ostringstream output;
    output << "HTTP/1.1 " << status << ' ' << reason << "\r\n"
           << "Content-Type: application/json\r\n"
           << "Cache-Control: no-store\r\n"
           << "Connection: close\r\n"
           << "Content-Length: " << body.size() << "\r\n\r\n"
           << body;
    return output.str();
}
} // namespace

class PalPanelBridge final : public RC::CppUserModBase
{
  public:
    PalPanelBridge()
    {
        ModName = STR("PalPanelBridge");
        ModVersion = STR("0.1.4");
        ModDescription = STR("Read-only localhost HTTP and UE4SS game-thread probe");
        ModAuthors = STR("PalPanel");
        ModIntendedSDKVersion = STR("3.0.1");
    }

    ~PalPanelBridge() override
    {
        stopping_.store(true);
        const auto listener = listener_.exchange(INVALID_SOCKET);
        if (listener != INVALID_SOCKET) closesocket(listener);
        if (worker_.joinable()) worker_.join();
    }

    auto on_unreal_init() -> void override
    {
        unreal_initialized_.store(true);
        bool expected = false;
        if (!server_started_.compare_exchange_strong(expected, true)) return;
        append_log("on_unreal_init received");
        config_ = load_config();
        try {
            worker_ = std::thread([this] { serve(); });
        } catch (...) {
            server_started_.store(false);
            append_log("failed to create HTTP worker thread");
        }
    }

    auto on_update() -> void override
    {
        game_thread_tick_seen_.store(true);
        std::scoped_lock lock(jobs_mutex_);
        for (auto& [_, job] : jobs_) {
            if (job.status != "queued") continue;
            job.unreal_initialized = unreal_initialized_.load();
            job.game_thread_tick_seen = true;
            job.status = "completed";
            break;
        }
    }

  private:
    Config config_{};
    std::atomic<bool> stopping_{false};
    std::atomic<bool> server_started_{false};
    std::atomic<bool> unreal_initialized_{false};
    std::atomic<bool> game_thread_tick_seen_{false};
    std::atomic<SOCKET> listener_{INVALID_SOCKET};
    std::atomic<unsigned long long> sequence_{0};
    std::thread worker_{};
    std::mutex jobs_mutex_{};
    std::map<std::string, Job> jobs_{};

    bool authorized(const std::string& request) const
    {
        if (config_.token.empty()) return false;
        return request.find("\r\nAuthorization: Bearer " + config_.token + "\r\n") != std::string::npos;
    }

    std::string health() const
    {
        std::ostringstream body;
        body << "{\"ok\":true,\"bridge_version\":\"0.1.4\",\"ue4ss_loaded\":true,"
             << "\"configured\":" << (config_.token.empty() ? "false" : "true") << ','
             << "\"unreal_initialized\":" << (unreal_initialized_.load() ? "true" : "false") << ','
             << "\"game_thread_tick_seen\":" << (game_thread_tick_seen_.load() ? "true" : "false") << '}';
        return body.str();
    }

    std::string enqueue()
    {
        const auto now = std::chrono::duration_cast<std::chrono::milliseconds>(
                             std::chrono::system_clock::now().time_since_epoch())
                             .count();
        const auto id = "probe_" + std::to_string(now) + "_" + std::to_string(++sequence_);
        std::scoped_lock lock(jobs_mutex_);
        if (jobs_.size() >= 64) jobs_.erase(jobs_.begin());
        jobs_.emplace(id, Job{.id = id});
        return id;
    }

    std::string get_job(const std::string& id)
    {
        std::scoped_lock lock(jobs_mutex_);
        const auto found = jobs_.find(id);
        if (found == jobs_.end()) return {};
        const auto& job = found->second;
        std::ostringstream body;
        body << "{\"ok\":true,\"job\":{\"id\":\"" << job.id << "\",\"status\":\"" << job.status
             << "\",\"result\":{\"unreal_initialized\":" << (job.unreal_initialized ? "true" : "false")
             << ",\"game_thread_tick_seen\":" << (job.game_thread_tick_seen ? "true" : "false") << "}}}";
        return body.str();
    }

    void handle(SOCKET client)
    {
        DWORD timeout = 2000;
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, reinterpret_cast<const char*>(&timeout), sizeof(timeout));
        char buffer[8193]{};
        const auto received = recv(client, buffer, 8192, 0);
        if (received <= 0) return;
        const std::string request(buffer, static_cast<size_t>(received));
        const auto first_end = request.find("\r\n");
        if (first_end == std::string::npos) return;
        const auto first = request.substr(0, first_end);

        int status = 404;
        std::string body{"{\"ok\":false,\"error\":{\"code\":\"not_found\",\"message\":\"route not found\"}}"};
        if (config_.token.empty()) {
            status = 503;
            body = "{\"ok\":false,\"error\":{\"code\":\"bridge_not_configured\",\"message\":\"bridge token is not configured\"}}";
        } else if (!authorized(request)) {
            status = 401;
            body = "{\"ok\":false,\"error\":{\"code\":\"unauthorized\",\"message\":\"valid bearer token required\"}}";
        } else if (first == "GET /v1/health HTTP/1.1") {
            status = 200;
            body = health();
        } else if (first == "POST /v1/probe/game-thread HTTP/1.1") {
            const auto id = enqueue();
            status = 202;
            body = "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first.rfind("GET /v1/jobs/", 0) == 0 && first.ends_with(" HTTP/1.1")) {
            const auto id = first.substr(13, first.size() - 13 - 9);
            body = get_job(id);
            status = body.empty() ? 404 : 200;
            if (body.empty()) {
                body = "{\"ok\":false,\"error\":{\"code\":\"job_not_found\",\"message\":\"probe job not found\"}}";
            }
        }
        const auto wire = response(status, body);
        send(client, wire.data(), static_cast<int>(wire.size()), 0);
    }

    void serve()
    {
        WSADATA data{};
        if (const auto error = WSAStartup(MAKEWORD(2, 2), &data); error != 0) {
            append_log("WSAStartup failed error=" + std::to_string(error));
            return;
        }
        const auto socket_handle = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (socket_handle == INVALID_SOCKET) {
            append_log("socket failed error=" + std::to_string(WSAGetLastError()));
            WSACleanup();
            return;
        }
        listener_.store(socket_handle);
        sockaddr_in address{};
        address.sin_family = AF_INET;
        address.sin_port = htons(config_.port);
        if (inet_pton(AF_INET, config_.listen.c_str(), &address.sin_addr) != 1) {
            append_log("invalid listen address");
            closesocket(socket_handle);
            listener_.store(INVALID_SOCKET);
            WSACleanup();
            return;
        }
        if (bind(socket_handle, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == SOCKET_ERROR) {
            append_log("bind failed port=" + std::to_string(config_.port) +
                       " error=" + std::to_string(WSAGetLastError()));
            closesocket(socket_handle);
            listener_.store(INVALID_SOCKET);
            WSACleanup();
            return;
        }
        if (listen(socket_handle, 8) == SOCKET_ERROR) {
            append_log("listen failed error=" + std::to_string(WSAGetLastError()));
            closesocket(socket_handle);
            listener_.store(INVALID_SOCKET);
            WSACleanup();
            return;
        }
        append_log("listening on " + config_.listen + ":" + std::to_string(config_.port));
        while (!stopping_.load()) {
            const auto client = accept(socket_handle, nullptr, nullptr);
            if (client == INVALID_SOCKET) {
                if (stopping_.load()) break;
                continue;
            }
            handle(client);
            closesocket(client);
        }
        const auto active = listener_.exchange(INVALID_SOCKET);
        if (active != INVALID_SOCKET) closesocket(active);
        WSACleanup();
    }
};

#define PALPANEL_BRIDGE_API __declspec(dllexport)
extern "C"
{
    PALPANEL_BRIDGE_API RC::CppUserModBase* start_mod()
    {
        return new PalPanelBridge();
    }

    PALPANEL_BRIDGE_API void uninstall_mod(RC::CppUserModBase* mod)
    {
        delete mod;
    }
}
