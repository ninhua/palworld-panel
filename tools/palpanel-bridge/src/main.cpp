#include <Mod/CppUserModBase.hpp>
#include <Helpers/String.hpp>
#include <Unreal/CoreUObject/UObject/Class.hpp>
#include <Unreal/CoreUObject/UObject/FStrProperty.hpp>
#include <Unreal/CoreUObject/UObject/UnrealType.hpp>
#include <Unreal/Core/Containers/Array.hpp>
#include <Unreal/FField.hpp>
#include <Unreal/UFunctionStructs.hpp>
#include <Unreal/UObject.hpp>
#include <Unreal/UObjectGlobals.hpp>

#include <WinSock2.h>
#include <WS2tcpip.h>
#include <Windows.h>

#include <atomic>
#include <array>
#include <chrono>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <initializer_list>
#include <map>
#include <mutex>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <unordered_set>
#include <utility>
#include <vector>

extern "C" IMAGE_DOS_HEADER __ImageBase;

namespace
{
struct Config
{
    std::string listen{"127.0.0.1"};
    unsigned short port{18083};
    std::string token{};
};

enum class JobKind
{
    GameThread,
    World,
    OnlinePlayers,
};

struct ObjectSnapshot
{
    std::string name{};
    std::string full_name{};
    std::string class_name{};
};

struct OnlinePlayerSnapshot
{
    std::string source{};
    ObjectSnapshot controller{};
    ObjectSnapshot player_state{};
    ObjectSnapshot pawn{};
    bool player_state_found{};
    bool pawn_found{};
    std::string account_name{};
    std::string player_uid{};
    std::string identity_error{};
};

struct Job
{
    std::string id;
    JobKind kind{JobKind::GameThread};
    std::string status{"queued"};
    unsigned long long queued_at_unix_ms{};
    unsigned long long executed_at_unix_ms{};
    unsigned long long game_thread_tick_count_at_execution{};
    bool unreal_initialized{};
    bool game_thread_tick_seen{};
    bool world_found{};
    std::string world_name{};
    std::string world_full_name{};
    std::string world_class_name{};
    size_t controller_object_count{};
    size_t player_state_object_count{};
    size_t pal_utility_player_state_count{};
    bool pal_utility_available{};
    std::string pal_utility_error{};
    ObjectSnapshot query_world{};
    bool query_world_found{};
    std::vector<OnlinePlayerSnapshot> online_players{};
};

struct PlayerGuid
{
    std::uint32_t a{};
    std::uint32_t b{};
    std::uint32_t c{};
    std::uint32_t d{};
};

ObjectSnapshot describe_object(RC::Unreal::UObject* object)
{
    ObjectSnapshot snapshot;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return snapshot;
    snapshot.name = RC::to_utf8_string(object->GetName());
    snapshot.full_name = RC::to_utf8_string(object->GetFullName());
    if (auto* object_class = object->GetClassPrivate(); object_class) {
        snapshot.class_name = RC::to_utf8_string(object_class->GetName());
    }
    return snapshot;
}

RC::Unreal::FProperty* find_struct_property(
    RC::Unreal::UStruct* owner, std::initializer_list<const TCHAR*> names)
{
    if (!owner) return nullptr;
    for (const auto* name : names) {
        if (auto* property = owner->FindProperty(
                RC::Unreal::FName(name, RC::Unreal::FNAME_Find)); property) {
            return property;
        }
    }
    return nullptr;
}

RC::Unreal::FProperty* find_property(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    if (!object || !RC::Unreal::UObject::IsReal(object)) return nullptr;
    return find_struct_property(object->GetClassPrivate(), names);
}

RC::Unreal::UObject* read_object_property(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    auto* property = find_property(object, names);
    auto* object_property = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(property);
    if (!object_property) return nullptr;
    auto* value = object_property->GetObjectPropertyValue(property->ContainerPtrToValuePtr<void>(object));
    return value && RC::Unreal::UObject::IsReal(value) ? value : nullptr;
}

bool read_player_guid(RC::Unreal::UObject* object, std::string& output)
{
    auto* property = find_property(object, {STR("PlayerUId"), STR("PlayerUID")});
    auto* struct_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(property);
    auto* structure = struct_property ? struct_property->GetStruct().Get() : nullptr;
    if (!property || !structure ||
        RC::to_utf8_string(structure->GetFullName()) != "ScriptStruct /Script/CoreUObject.Guid" ||
        property->GetSize() < static_cast<std::int32_t>(sizeof(PlayerGuid))) {
        return false;
    }
    PlayerGuid value{};
    std::memcpy(&value, property->ContainerPtrToValuePtr<void>(object), sizeof(value));
    std::array<char, 33> buffer{};
    std::snprintf(buffer.data(), buffer.size(), "%08X%08X%08X%08X", value.a, value.b, value.c, value.d);
    output = buffer.data();
    return true;
}

bool read_account_name(RC::Unreal::UObject* object, std::string& output)
{
    auto* property = find_property(object, {STR("AccountName")});
    auto* string_property = RC::Unreal::CastField<RC::Unreal::FStrProperty>(property);
    if (!string_property) return false;
    const auto& value = string_property->GetPropertyValueInContainer(object);
    output = RC::to_utf8_string(*value);
    return true;
}

void append_instances(
    std::string_view class_name,
    std::vector<RC::Unreal::UObject*>& output,
    std::unordered_set<RC::Unreal::UObject*>& seen)
{
    try {
        std::vector<RC::Unreal::UObject*> found;
        RC::Unreal::UObjectGlobals::FindAllOf(class_name, found);
        for (auto* object : found) {
            if (object && RC::Unreal::UObject::IsReal(object) && seen.insert(object).second) {
                output.emplace_back(object);
            }
        }
    } catch (...) {
    }
}

void append_pal_utility_player_states(
    RC::Unreal::UObject* world,
    std::vector<RC::Unreal::UObject*>& output,
    std::unordered_set<RC::Unreal::UObject*>& seen,
    bool& available,
    std::string& error)
{
    using namespace RC::Unreal;
    available = false;
    error.clear();
    try {
        auto* utility = UObjectGlobals::StaticFindObject<UObject*>(
            nullptr, nullptr, STR("/Script/Pal.Default__PalUtility"));
        auto* function = UObjectGlobals::StaticFindObject<UFunction*>(
            nullptr, nullptr, STR("/Script/Pal.PalUtility:GetAllPlayerStates"));
        if (!world || !UObject::IsReal(world)) {
            error = "World context is unavailable";
            return;
        }
        if (!utility || !UObject::IsReal(utility) || !function || !UObject::IsReal(function)) {
            error = "PalUtility.GetAllPlayerStates is unavailable";
            return;
        }
        auto* context_property = find_struct_property(function, {STR("WorldContextObject")});
        auto* context_object_property = CastField<FObjectPropertyBase>(context_property);
        auto* states_property = find_struct_property(function, {STR("OutPlayerStates")});
        auto* states_array_property = CastField<FArrayProperty>(states_property);
        auto* inner_object_property = states_array_property
                                          ? CastField<FObjectPropertyBase>(states_array_property->GetInner())
                                          : nullptr;
        auto* player_state_class = inner_object_property
                                       ? inner_object_property->GetPropertyClass().Get()
                                       : nullptr;
        if (!context_property || !context_object_property || !states_property || !states_array_property ||
            !player_state_class ||
            states_property->GetSize() < static_cast<std::int32_t>(sizeof(TArray<UObject*>))) {
            error = "PalUtility.GetAllPlayerStates parameters do not match this game build";
            return;
        }
        std::vector<std::uint8_t> parameters(
            static_cast<size_t>(function->GetParmsSize()), std::uint8_t{0});
        context_object_property->SetObjectPropertyValue(
            context_property->ContainerPtrToValuePtr<void>(parameters.data()), world);
        utility->ProcessEvent(function, parameters.data());
        auto* states = static_cast<TArray<UObject*>*>(
            states_property->ContainerPtrToValuePtr<void>(parameters.data()));
        const auto count = states->Num();
        if (count < 0 || count > 1024) {
            states_property->DestroyValue_InContainer(parameters.data());
            error = "PalUtility.GetAllPlayerStates returned an invalid array size";
            return;
        }
        for (TArray<UObject*>::SizeType index = 0; index < count; ++index) {
            auto* state = (*states)[index];
            if (state && UObject::IsReal(state) && state->IsA(player_state_class) && seen.insert(state).second) {
                output.emplace_back(state);
            }
        }
        states_property->DestroyValue_InContainer(parameters.data());
        available = true;
    } catch (...) {
        error = "PalUtility.GetAllPlayerStates invocation failed";
    }
}

void populate_player_state(RC::Unreal::UObject* player_state, OnlinePlayerSnapshot& player)
{
    player.player_state_found = player_state != nullptr;
    if (!player_state) {
        player.identity_error = "PlayerState is unavailable";
        return;
    }
    player.player_state = describe_object(player_state);
    const auto uid_ok = read_player_guid(player_state, player.player_uid);
    const auto name_ok = read_account_name(player_state, player.account_name);
    if (!uid_ok || !name_ok) {
        player.identity_error = !uid_ok && !name_ok
                                    ? "PlayerUId and AccountName are unavailable"
                                    : !uid_ok ? "PlayerUId is unavailable" : "AccountName is unavailable";
    }
}

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

unsigned long long unix_time_ms()
{
    return static_cast<unsigned long long>(std::chrono::duration_cast<std::chrono::milliseconds>(
                                               std::chrono::system_clock::now().time_since_epoch())
                                               .count());
}

std::string utc_time(unsigned long long milliseconds)
{
    if (milliseconds == 0) return {};
    const auto seconds = static_cast<std::time_t>(milliseconds / 1000);
    std::tm utc{};
    if (gmtime_s(&utc, &seconds) != 0) return {};
    std::ostringstream output;
    output << std::put_time(&utc, "%Y-%m-%dT%H:%M:%S") << '.' << std::setw(3) << std::setfill('0')
           << (milliseconds % 1000) << 'Z';
    return output.str();
}

std::string json_escape(const std::string& value)
{
    static constexpr char hex[] = "0123456789abcdef";
    std::string escaped;
    escaped.reserve(value.size());
    for (const auto character : value) {
        const auto byte = static_cast<unsigned char>(character);
        if (byte == '"' || byte == '\\') {
            escaped.push_back('\\');
            escaped.push_back(static_cast<char>(byte));
        } else if (byte < 0x20) {
            escaped.append("\\u00");
            escaped.push_back(hex[byte >> 4]);
            escaped.push_back(hex[byte & 0x0f]);
        } else {
            escaped.push_back(static_cast<char>(byte));
        }
    }
    return escaped;
}

const char* job_kind_name(JobKind kind)
{
    if (kind == JobKind::World) return "world";
    if (kind == JobKind::OnlinePlayers) return "online_players";
    return "game_thread";
}

std::string add_response_time(const std::string& body)
{
    if (body.empty() || body.front() != '{') return body;
    const auto now = unix_time_ms();
    std::ostringstream output;
    output << "{\"response_time_unix_ms\":" << now << ",\"response_time_utc\":\""
           << utc_time(now) << '"';
    if (body.size() > 2) output << ',' << body.substr(1);
    else output << '}';
    return output.str();
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
        ModVersion = STR("0.1.11");
        ModDescription = STR("Read-only localhost HTTP and UE object diagnostics");
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
        game_thread_tick_count_.fetch_add(1, std::memory_order_relaxed);
        last_game_thread_tick_unix_ms_.store(unix_time_ms(), std::memory_order_relaxed);
        std::scoped_lock lock(jobs_mutex_);
        for (auto& [_, job] : jobs_) {
            if (job.status != "queued") continue;
            job.executed_at_unix_ms = unix_time_ms();
            job.game_thread_tick_count_at_execution =
                game_thread_tick_count_.load(std::memory_order_relaxed);
            job.unreal_initialized = unreal_initialized_.load();
            job.game_thread_tick_seen = true;
            if (job.kind == JobKind::World) {
                try {
                    auto* world = RC::Unreal::UObjectGlobals::FindFirstOf(STR("World"));
                    job.world_found = world != nullptr;
                    if (world) {
                        job.world_name = RC::to_utf8_string(world->GetName());
                        job.world_full_name = RC::to_utf8_string(world->GetFullName());
                        if (auto* world_class = world->GetClassPrivate(); world_class) {
                            job.world_class_name = RC::to_utf8_string(world_class->GetName());
                        }
                    }
                } catch (...) {
                    job.status = "failed";
                    break;
                }
            } else if (job.kind == JobKind::OnlinePlayers) {
                try {
                    std::vector<RC::Unreal::UObject*> controllers;
                    std::unordered_set<RC::Unreal::UObject*> seen_controllers;
                    append_instances("PalPlayerController", controllers, seen_controllers);
                    append_instances("BP_PalPlayerController_C", controllers, seen_controllers);
                    job.controller_object_count = controllers.size();
                    std::unordered_set<RC::Unreal::UObject*> seen_player_states;
                    constexpr size_t max_results = 64;
                    for (auto* controller : controllers) {
                        if (!controller || job.online_players.size() >= max_results) continue;
                        OnlinePlayerSnapshot player;
                        player.source = "controller";
                        player.controller = describe_object(controller);
                        auto* player_state = read_object_property(controller, {STR("PlayerState")});
                        if (player_state) seen_player_states.insert(player_state);
                        populate_player_state(player_state, player);
                        auto* pawn = read_object_property(controller, {STR("AcknowledgedPawn")});
                        if (!pawn) pawn = read_object_property(controller, {STR("Pawn")});
                        player.pawn_found = pawn != nullptr;
                        if (pawn) player.pawn = describe_object(pawn);
                        job.online_players.emplace_back(std::move(player));
                    }
                    std::vector<RC::Unreal::UObject*> player_states;
                    std::unordered_set<RC::Unreal::UObject*> all_player_states;
                    auto* world = RC::Unreal::UObjectGlobals::FindFirstOf(STR("World"));
                    job.query_world_found = world && RC::Unreal::UObject::IsReal(world);
                    if (job.query_world_found) job.query_world = describe_object(world);
                    std::vector<RC::Unreal::UObject*> utility_player_states;
                    std::unordered_set<RC::Unreal::UObject*> utility_seen;
                    append_pal_utility_player_states(
                        world,
                        utility_player_states,
                        utility_seen,
                        job.pal_utility_available,
                        job.pal_utility_error);
                    job.pal_utility_player_state_count = utility_player_states.size();
                    for (auto* player_state : utility_player_states) {
                        if (job.online_players.size() >= max_results) break;
                        if (!seen_player_states.insert(player_state).second) continue;
                        OnlinePlayerSnapshot player;
                        player.source = "pal_utility";
                        populate_player_state(player_state, player);
                        job.online_players.emplace_back(std::move(player));
                    }
                    append_instances("PalPlayerState", player_states, all_player_states);
                    append_instances("BP_PalPlayerState_C", player_states, all_player_states);
                    job.player_state_object_count = player_states.size();
                    for (auto* player_state : player_states) {
                        if (job.online_players.size() >= max_results) break;
                        if (!seen_player_states.insert(player_state).second) continue;
                        OnlinePlayerSnapshot player;
                        player.source = "player_state_fallback";
                        populate_player_state(player_state, player);
                        job.online_players.emplace_back(std::move(player));
                    }
                } catch (...) {
                    job.status = "failed";
                    break;
                }
            }
            job.status = "completed";
            break;
        }
    }

  private:
    const unsigned long long started_at_unix_ms_{unix_time_ms()};
    Config config_{};
    std::atomic<bool> stopping_{false};
    std::atomic<bool> server_started_{false};
    std::atomic<bool> unreal_initialized_{false};
    std::atomic<bool> game_thread_tick_seen_{false};
    std::atomic<unsigned long long> game_thread_tick_count_{0};
    std::atomic<unsigned long long> last_game_thread_tick_unix_ms_{0};
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
        body << "{\"ok\":true,\"bridge_version\":\"0.1.11\",\"ue4ss_loaded\":true,"
             << "\"configured\":" << (config_.token.empty() ? "false" : "true") << ','
             << "\"unreal_initialized\":" << (unreal_initialized_.load() ? "true" : "false") << ','
             << "\"game_thread_tick_seen\":" << (game_thread_tick_seen_.load() ? "true" : "false") << '}';
        return body.str();
    }

    std::string runtime() const
    {
        const auto now = unix_time_ms();
        const auto last_tick = last_game_thread_tick_unix_ms_.load(std::memory_order_relaxed);
        const auto started = started_at_unix_ms_;
        std::ostringstream body;
        body << "{\"ok\":true,\"bridge_version\":\"0.1.11\","
             << "\"unreal_initialized\":" << (unreal_initialized_.load() ? "true" : "false") << ','
             << "\"game_thread_tick_count\":" << game_thread_tick_count_.load(std::memory_order_relaxed) << ','
             << "\"last_game_thread_tick_unix_ms\":" << last_tick << ','
             << "\"last_game_thread_tick_age_ms\":" << (last_tick > 0 && now >= last_tick ? now - last_tick : 0) << ','
             << "\"bridge_uptime_ms\":" << (now >= started ? now - started : 0) << '}';
        return body.str();
    }

    std::string enqueue(JobKind kind)
    {
        const auto now = std::chrono::duration_cast<std::chrono::milliseconds>(
                             std::chrono::system_clock::now().time_since_epoch())
                             .count();
        const auto prefix = kind == JobKind::World
                                ? "world_"
                                : kind == JobKind::OnlinePlayers ? "players_" : "probe_";
        const auto id = std::string(prefix) + std::to_string(now) + "_" + std::to_string(++sequence_);
        std::scoped_lock lock(jobs_mutex_);
        if (jobs_.size() >= 64) jobs_.erase(jobs_.begin());
        jobs_.emplace(id, Job{.id = id, .kind = kind, .queued_at_unix_ms = static_cast<unsigned long long>(now)});
        return id;
    }

    std::string get_job(const std::string& id)
    {
        std::scoped_lock lock(jobs_mutex_);
        const auto found = jobs_.find(id);
        if (found == jobs_.end()) return {};
        const auto& job = found->second;
        std::ostringstream body;
        body << "{\"ok\":true,\"job\":{\"id\":\"" << job.id << "\",\"type\":\"" << job_kind_name(job.kind)
             << "\",\"status\":\"" << job.status << "\",\"queued_at_unix_ms\":" << job.queued_at_unix_ms
             << ",\"queued_at_utc\":\"" << utc_time(job.queued_at_unix_ms)
             << "\",\"executed_at_unix_ms\":" << job.executed_at_unix_ms
             << ",\"executed_at_utc\":\"" << utc_time(job.executed_at_unix_ms)
             << "\",\"game_thread_tick_count_at_execution\":" << job.game_thread_tick_count_at_execution
             << ",\"result\":{\"unreal_initialized\":" << (job.unreal_initialized ? "true" : "false")
             << ",\"game_thread_tick_seen\":" << (job.game_thread_tick_seen ? "true" : "false");
        if (job.kind == JobKind::World) {
            body << ",\"world_found\":" << (job.world_found ? "true" : "false") << ",\"world_name\":\""
                 << json_escape(job.world_name) << "\",\"world_full_name\":\"" << json_escape(job.world_full_name)
                 << "\",\"world_class_name\":\"" << json_escape(job.world_class_name) << '"';
        } else if (job.kind == JobKind::OnlinePlayers) {
            body << ",\"controller_object_count\":" << job.controller_object_count
                 << ",\"player_state_object_count\":" << job.player_state_object_count
                 << ",\"pal_utility_available\":" << (job.pal_utility_available ? "true" : "false")
                 << ",\"pal_utility_player_state_count\":" << job.pal_utility_player_state_count
                 << ",\"pal_utility_error\":\"" << json_escape(job.pal_utility_error) << '"'
                 << ",\"query_world_found\":" << (job.query_world_found ? "true" : "false")
                 << ",\"query_world\":{\"name\":\"" << json_escape(job.query_world.name)
                 << "\",\"full_name\":\"" << json_escape(job.query_world.full_name)
                 << "\",\"class_name\":\"" << json_escape(job.query_world.class_name) << "\"}"
                 << ",\"online_player_count\":" << job.online_players.size() << ",\"players\":[";
            for (size_t index = 0; index < job.online_players.size(); ++index) {
                if (index > 0) body << ',';
                const auto& player = job.online_players[index];
                body << "{\"source\":\"" << json_escape(player.source) << "\",\"name\":\""
                     << json_escape(player.controller.name) << "\",\"full_name\":\""
                     << json_escape(player.controller.full_name) << "\",\"class_name\":\""
                     << json_escape(player.controller.class_name) << "\",\"player_state_found\":"
                     << (player.player_state_found ? "true" : "false") << ",\"player_state\":{\"name\":\""
                     << json_escape(player.player_state.name) << "\",\"full_name\":\""
                     << json_escape(player.player_state.full_name) << "\",\"class_name\":\""
                     << json_escape(player.player_state.class_name) << "\"},\"account_name\":\""
                     << json_escape(player.account_name) << "\",\"player_uid\":\""
                     << json_escape(player.player_uid) << "\",\"identity_error\":\""
                     << json_escape(player.identity_error) << "\",\"pawn_found\":"
                     << (player.pawn_found ? "true" : "false") << ",\"pawn\":{\"name\":\""
                     << json_escape(player.pawn.name) << "\",\"full_name\":\""
                     << json_escape(player.pawn.full_name) << "\",\"class_name\":\""
                     << json_escape(player.pawn.class_name) << "\"}}";
            }
            body << ']';
        }
        body << "}}}";
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
        } else if (first == "GET /v1/runtime HTTP/1.1") {
            status = 200;
            body = runtime();
        } else if (first == "POST /v1/world HTTP/1.1") {
            const auto id = enqueue(JobKind::World);
            status = 202;
            body = "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/players/online HTTP/1.1") {
            const auto id = enqueue(JobKind::OnlinePlayers);
            status = 202;
            body = "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/probe/game-thread HTTP/1.1") {
            const auto id = enqueue(JobKind::GameThread);
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
        body = add_response_time(body);
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
