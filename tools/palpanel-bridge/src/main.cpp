#include <Mod/CppUserModBase.hpp>
#include <Helpers/String.hpp>
#include <Unreal/CoreUObject/UObject/Class.hpp>
#include <Unreal/CoreUObject/UObject/FStrProperty.hpp>
#include <Unreal/CoreUObject/UObject/UnrealType.hpp>
#include <Unreal/Core/Containers/Array.hpp>
#include <Unreal/FField.hpp>
#include <Unreal/NameTypes.hpp>
#include <Unreal/UFunctionStructs.hpp>
#include <Unreal/UObject.hpp>
#include <Unreal/UObjectGlobals.hpp>

#include <WinSock2.h>
#include <WS2tcpip.h>
#include <Windows.h>

#include <atomic>
#include <algorithm>
#include <array>
#include <cctype>
#include <chrono>
#include <climits>
#include <cmath>
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
    Mutation,
};

struct MutationRequest
{
    std::string operation{};
    bool confirm{};
    std::string player_uid{};
    std::string instance_id{};
    std::int32_t container_index{-1};
    std::int32_t slot_index{-1};
    std::int32_t expected_stack_count{-1};
    std::int32_t stack_count{-1};
    std::string expected_item_static_id{};
    std::string expected_character_id{};
    std::string passive_skill_id{};
    bool add_passive{};
    std::vector<std::string> expected_passive_skill_ids{};
    std::string expected_value_field{};
    std::int32_t expected_value{-1};
    std::string value_field{};
    std::int32_t value{-1};
};

struct MutationResult
{
    std::string operation{};
    std::string status{"rejected"};
    std::string before_json{"null"};
    std::string after_json{"null"};
    std::string error{};
    std::string rollback_status{"not_attempted"};
    std::string rollback_error{};
};

struct ObjectSnapshot
{
    std::string name{};
    std::string full_name{};
    std::string class_name{};
};

struct CachedLocationSnapshot
{
    bool found{};
    std::array<double, 3> value{};
    std::string error{};
};

struct PalSlotSnapshot
{
    bool found{};
    std::string individual_id{};
    bool slot_object_found{};
    ObjectSnapshot slot_object{};
    bool handle_found{};
    ObjectSnapshot handle{};
    bool replicate_parameter_found{};
    ObjectSnapshot replicate_parameter{};
    std::string handle_player_uid{};
    std::string replicate_handle_id_hex{};
    bool level_found{};
    double level{};
    bool rank_found{};
    double rank{};
    bool experience_found{};
    double experience{};
    bool hp_found{};
    double hp{};
    bool max_hp_found{};
    double max_hp{};
    bool full_stomach_found{};
    double full_stomach{};
    bool sanity_found{};
    double sanity{};
    std::string nickname{};
    std::string character_id{};
    std::vector<std::string> passive_skill_ids{};
    std::vector<std::uint16_t> equipped_waza_ids{};
};

struct PalSlotArraySnapshot
{
    bool found{};
    std::int32_t slot_count{-1};
    std::vector<PalSlotSnapshot> slots{};
};

struct PropertyCandidateSnapshot;

struct ItemSlotSnapshot
{
    bool found{};
    bool slot_index_found{};
    double slot_index{};
    bool stack_count_found{};
    double stack_count{};
    std::string item_static_id{};
};

struct ItemContainerSnapshot
{
    bool found{};
    ObjectSnapshot container{};
    std::int32_t slot_count{-1};
    std::vector<ItemSlotSnapshot> slots{};
    std::vector<PropertyCandidateSnapshot> container_property_metadata{};
};

struct FunctionCandidateSnapshot
{
    std::string name{};
    std::string full_name{};
    std::int32_t params_size{};
    struct Parameter
    {
        std::string name{};
        std::string kind{};
        std::string declared_type{};
        std::int32_t size{};
        bool return_value{};
    };
    std::vector<Parameter> parameters{};
};

struct PropertyCandidateSnapshot
{
    std::string name{};
    std::string kind{};
    std::string declared_type{};
    bool object_value_found{};
    ObjectSnapshot object_value{};
    bool collection_count_available{};
    std::int32_t collection_count{};
    std::vector<PropertyCandidateSnapshot> nested_candidates{};
    std::vector<FunctionCandidateSnapshot> function_candidates{};
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
    bool player_data_ready{};
    CachedLocationSnapshot cached_location{};
    bool guild_found{};
    ObjectSnapshot guild{};
    std::string guild_name{};
    std::string guild_admin_player_uid{};
    std::int32_t base_camp_count{-1};
    bool base_camp_level_found{};
    double base_camp_level{};
    bool inventory_found{};
    ObjectSnapshot inventory{};
    std::int32_t inventory_container_count{-1};
    bool inventory_weight_found{};
    double now_item_weight{};
    double max_inventory_weight{};
    bool pal_storage_found{};
    ObjectSnapshot pal_storage{};
    bool pal_container_found{};
    ObjectSnapshot pal_container{};
    PalSlotArraySnapshot pal_slot_array{};
    PalSlotArraySnapshot pal_non_empty_slot_array{};
    bool inventory_helper_found{};
    ObjectSnapshot inventory_helper{};
    bool otomo_found{};
    ObjectSnapshot otomo{};
    std::vector<ItemContainerSnapshot> inventory_containers{};
    bool character_parameter_found{};
    ObjectSnapshot character_parameter{};
    std::vector<PropertyCandidateSnapshot> controller_property_candidates{};
    std::vector<PropertyCandidateSnapshot> player_state_property_candidates{};
    std::vector<PropertyCandidateSnapshot> pawn_property_candidates{};
    std::vector<PropertyCandidateSnapshot> player_state_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pawn_property_metadata{};
    std::vector<PropertyCandidateSnapshot> guild_property_metadata{};
    std::vector<PropertyCandidateSnapshot> character_parameter_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_all_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_storage_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_container_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_helper_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_container_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_slot_property_metadata{};
    std::vector<PropertyCandidateSnapshot> inventory_item_id_struct_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_slot_object_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_handle_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_parameter_property_metadata{};
    std::vector<PropertyCandidateSnapshot> pal_handle_id_struct_metadata{};
    std::vector<PropertyCandidateSnapshot> base_camp_id_struct_metadata{};
    std::vector<PropertyCandidateSnapshot> otomo_property_metadata{};
    bool detail_property_metadata_collected{};
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
    bool game_state_found{};
    ObjectSnapshot game_state{};
    bool game_state_player_array_available{};
    size_t game_state_player_state_count{};
    std::string game_state_error{};
    std::vector<OnlinePlayerSnapshot> online_players{};
    bool metadata_probe{false};
    size_t metadata_player_count{};
    bool metadata_truncated{false};
    MutationRequest mutation_request{};
    MutationResult mutation_result{};
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
    auto* address = property->ContainerPtrToValuePtr<void>(object);
    if (!address) return nullptr;
    auto* value = object_property->GetObjectPropertyValue(address);
    return value && RC::Unreal::UObject::IsReal(value) ? value : nullptr;
}

PropertyCandidateSnapshot describe_property_candidate(RC::Unreal::FProperty* property)
{
    PropertyCandidateSnapshot snapshot;
    if (!property) return snapshot;
    snapshot.name = RC::to_utf8_string(property->GetName());
    if (auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property)) {
        snapshot.kind = "array";
        if (auto* inner_object = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(array_property->GetInner())) {
            if (auto* object_class = inner_object->GetPropertyClass().Get(); object_class) {
                snapshot.declared_type = RC::to_utf8_string(object_class->GetName());
            }
        }
    } else if (RC::Unreal::CastField<RC::Unreal::FMapProperty>(property)) {
        snapshot.kind = "map";
    } else if (auto* object_property = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(property)) {
        snapshot.kind = "object";
        if (auto* object_class = object_property->GetPropertyClass().Get(); object_class) {
            snapshot.declared_type = RC::to_utf8_string(object_class->GetName());
        }
    } else if (auto* struct_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(property)) {
        snapshot.kind = "struct";
        if (auto* structure = struct_property->GetStruct().Get(); structure) {
            snapshot.declared_type = RC::to_utf8_string(structure->GetName());
        }
    } else {
        snapshot.kind = "other";
    }
    return snapshot;
}

std::vector<FunctionCandidateSnapshot> collect_player_data_function_candidates(
    RC::Unreal::UObject* object)
{
    std::vector<FunctionCandidateSnapshot> candidates;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return candidates;
    static constexpr std::array<std::string_view, 11> keywords{
        "inventory", "container", "equipment", "equip", "otomo", "party",
        "item", "slot", "pal", "loadout", "select"};
    std::unordered_set<std::string> seen;
    try {
        for (auto* function : RC::Unreal::TFieldRange<RC::Unreal::UFunction>(
                 object->GetClassPrivate(), RC::Unreal::EFieldIterationFlags::IncludeAll)) {
            if (!function || candidates.size() >= 64) continue;
            auto name = RC::to_utf8_string(function->GetName());
            auto lower = name;
            std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char character) {
                return static_cast<char>(std::tolower(character));
            });
            auto selected = false;
            for (const auto keyword : keywords) {
                if (lower.find(keyword) != std::string::npos) {
                    selected = true;
                    break;
                }
            }
            if (!selected || !seen.insert(name).second) continue;
            FunctionCandidateSnapshot candidate{
                .name = std::move(name),
                .full_name = RC::to_utf8_string(function->GetFullName()),
                .params_size = function->GetParmsSize(),
            };
            candidates.emplace_back(std::move(candidate));
        }
    } catch (...) {
    }
    return candidates;
}

std::vector<PropertyCandidateSnapshot> collect_player_data_property_candidates(
    RC::Unreal::UObject* object, bool inspect_object_values = true, bool include_all_properties = false)
{
    std::vector<PropertyCandidateSnapshot> candidates;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return candidates;
    static constexpr std::array<std::string_view, 9> keywords{
        "inventory", "container", "equipment", "equip", "otomo", "party", "item", "slot", "pal"};
    std::unordered_set<std::string> seen;
    try {
        for (auto* property : object->GetClassPrivate()->ForEachProperty()) {
            const auto max_candidates = include_all_properties ? size_t{96} : size_t{64};
            if (!property || candidates.size() >= max_candidates) continue;
            auto name = RC::to_utf8_string(property->GetName());
            auto lower = name;
            std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char character) {
                return static_cast<char>(std::tolower(character));
            });
            auto selected = include_all_properties;
            for (const auto keyword : keywords) {
                if (lower.find(keyword) != std::string::npos) selected = true;
            }
            if (!selected || !seen.insert(name).second) continue;
            auto candidate = describe_property_candidate(property);
            if (auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property)) {
                RC::Unreal::FScriptArrayHelper_InContainer values(array_property, object);
                const auto count = values.Num();
                if (count >= 0 && count <= 100000) {
                    candidate.collection_count_available = true;
                    candidate.collection_count = count;
                }
            } else if (RC::Unreal::CastField<RC::Unreal::FMapProperty>(property)) {
                auto* values = static_cast<RC::Unreal::FScriptMap*>(
                    property->ContainerPtrToValuePtr<void>(object));
                const auto count = values ? values->Num() : -1;
                if (count >= 0 && count <= 100000) {
                    candidate.collection_count_available = true;
                    candidate.collection_count = count;
                }
            }
            if (inspect_object_values && candidate.kind == "object") {
                auto* object_property = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(property);
                auto* value = object_property
                                  ? object_property->GetObjectPropertyValue(
                                        property->ContainerPtrToValuePtr<void>(object))
                                  : nullptr;
                candidate.object_value_found = value && RC::Unreal::UObject::IsReal(value);
                if (candidate.object_value_found) {
                    candidate.object_value = describe_object(value);
                    auto nested_class = candidate.object_value.class_name;
                    std::transform(nested_class.begin(), nested_class.end(), nested_class.begin(), [](unsigned char character) {
                        return static_cast<char>(std::tolower(character));
                    });
                    const auto expand_all = nested_class == "palitemselectorcomponent" ||
                                            nested_class.find("otomopalholdercomponent") != std::string::npos;
                    candidate.nested_candidates =
                        collect_player_data_property_candidates(value, false, expand_all);
                    if (expand_all) {
                        candidate.function_candidates = collect_player_data_function_candidates(value);
                    }
                }
            }
            candidates.emplace_back(std::move(candidate));
        }
    } catch (...) {
    }
    return candidates;
}

std::vector<PropertyCandidateSnapshot> collect_top_level_property_metadata(
    RC::Unreal::UObject* object)
{
    constexpr size_t metadata_limit = 96;
    std::vector<PropertyCandidateSnapshot> metadata;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return metadata;
    auto* object_class = object->GetClassPrivate();
    if (!object_class) return metadata;
    try {
        for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
                 object_class, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
            if (metadata.size() >= metadata_limit) return metadata;
            if (!property) continue;
            metadata.emplace_back(describe_property_candidate(property));
        }
        for (auto* parent_class : RC::Unreal::TSuperStructRange(object_class)) {
            for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
                     parent_class, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
                if (metadata.size() >= metadata_limit) return metadata;
                if (!property) continue;
                metadata.emplace_back(describe_property_candidate(property));
            }
        }
    } catch (...) {
    }
    return metadata;
}

std::vector<PropertyCandidateSnapshot> collect_keyword_property_metadata(
    RC::Unreal::UObject* object, std::initializer_list<std::string_view> keywords)
{
    constexpr size_t metadata_limit = 64;
    std::vector<PropertyCandidateSnapshot> metadata;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return metadata;
    auto* object_class = object->GetClassPrivate();
    if (!object_class) return metadata;
    std::unordered_set<std::string> seen;
    const auto collect_class = [&](RC::Unreal::UStruct* owner) {
        for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
                 owner, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
            if (metadata.size() >= metadata_limit) return false;
            if (!property) continue;
            auto name = RC::to_utf8_string(property->GetName());
            auto lower = name;
            std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char character) {
                return static_cast<char>(std::tolower(character));
            });
            const auto selected = std::any_of(keywords.begin(), keywords.end(),
                                              [&](const auto keyword) {
                                                  return lower.find(keyword) != std::string::npos;
                                              });
            if (!selected || !seen.insert(name).second) continue;
            metadata.emplace_back(describe_property_candidate(property));
        }
        return metadata.size() < metadata_limit;
    };
    try {
        if (!collect_class(object_class)) return metadata;
        for (auto* parent_class : RC::Unreal::TSuperStructRange(object_class)) {
            if (!collect_class(parent_class)) break;
        }
    } catch (...) {
    }
    return metadata;
}

std::vector<PropertyCandidateSnapshot> collect_data_property_metadata(
    RC::Unreal::UObject* object, std::initializer_list<std::string_view> keywords)
{
    constexpr size_t metadata_limit = 96;
    std::vector<PropertyCandidateSnapshot> metadata;
    if (!object || !RC::Unreal::UObject::IsReal(object)) return metadata;
    auto* object_class = object->GetClassPrivate();
    if (!object_class) return metadata;
    std::unordered_set<std::string> seen;
    const auto collect_class = [&](RC::Unreal::UStruct* owner) {
        for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
                 owner, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
            if (metadata.size() >= metadata_limit) return false;
            if (!property) continue;
            auto name = RC::to_utf8_string(property->GetName());
            auto lower = name;
            std::transform(lower.begin(), lower.end(), lower.begin(), [](unsigned char character) {
                return static_cast<char>(std::tolower(character));
            });
            if (lower.find("delegate") != std::string::npos ||
                lower.rfind("onupdate", 0) == 0 || lower.rfind("onchanged", 0) == 0) {
                continue;
            }
            const auto selected = std::any_of(keywords.begin(), keywords.end(),
                                              [&](const auto keyword) {
                                                  return lower.find(keyword) != std::string::npos;
                                              });
            if (!selected || !seen.insert(name).second) continue;
            metadata.emplace_back(describe_property_candidate(property));
        }
        return metadata.size() < metadata_limit;
    };
    try {
        if (!collect_class(object_class)) return metadata;
        for (auto* parent_class : RC::Unreal::TSuperStructRange(object_class)) {
            if (!collect_class(parent_class)) break;
        }
    } catch (...) {
    }
    return metadata;
}

std::vector<PropertyCandidateSnapshot> collect_struct_metadata(RC::Unreal::UStruct* structure)
{
    constexpr size_t metadata_limit = 64;
    std::vector<PropertyCandidateSnapshot> metadata;
    if (!structure) return metadata;
    try {
        for (auto* property : structure->ForEachProperty()) {
            if (!property || metadata.size() >= metadata_limit) break;
            metadata.emplace_back(describe_property_candidate(property));
        }
    } catch (...) {
    }
    return metadata;
}

std::vector<PropertyCandidateSnapshot> collect_struct_property_metadata(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    auto* property = find_property(object, names);
    auto* struct_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(property);
    return collect_struct_metadata(struct_property ? struct_property->GetStruct().Get() : nullptr);
}

std::vector<PropertyCandidateSnapshot> collect_array_element_metadata(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    auto* property = find_property(object, names);
    auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
    if (!array_property) return {};
    if (auto* inner_struct = RC::Unreal::CastField<RC::Unreal::FStructProperty>(array_property->GetInner())) {
        return collect_struct_metadata(inner_struct->GetStruct().Get());
    }
    auto* inner_object = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(array_property->GetInner());
    if (!inner_object) return {};
    RC::Unreal::FScriptArrayHelper_InContainer values(array_property, object);
    if (values.Num() <= 0) return {};
    auto* element = values.GetRawPtr(0);
    auto* value = element ? inner_object->GetObjectPropertyValue(element) : nullptr;
    return value && RC::Unreal::UObject::IsReal(value)
               ? collect_top_level_property_metadata(value)
               : std::vector<PropertyCandidateSnapshot>{};
}

bool read_guid_property(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names, std::string& output)
{
    auto* property = find_property(object, names);
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

bool read_nested_guid_property(
    RC::Unreal::UObject* object,
    std::initializer_list<const TCHAR*> outer_names,
    std::initializer_list<const TCHAR*> inner_names,
    std::string& output)
{
    auto* outer_property = find_property(object, outer_names);
    auto* outer_struct_property =
        RC::Unreal::CastField<RC::Unreal::FStructProperty>(outer_property);
    auto* outer_struct = outer_struct_property ? outer_struct_property->GetStruct().Get() : nullptr;
    auto* outer_value = outer_property
                            ? outer_property->ContainerPtrToValuePtr<void>(object)
                            : nullptr;
    auto* inner_property = outer_struct
                               ? find_struct_property(outer_struct, inner_names)
                               : nullptr;
    auto* inner_struct_property =
        RC::Unreal::CastField<RC::Unreal::FStructProperty>(inner_property);
    auto* inner_struct = inner_struct_property ? inner_struct_property->GetStruct().Get() : nullptr;
    if (!outer_value || !inner_property || !inner_struct ||
        RC::to_utf8_string(inner_struct->GetFullName()) != "ScriptStruct /Script/CoreUObject.Guid" ||
        inner_property->GetSize() < static_cast<std::int32_t>(sizeof(PlayerGuid))) {
        return false;
    }
    PlayerGuid value{};
    std::memcpy(
        &value,
        inner_property->ContainerPtrToValuePtr<void>(outer_value),
        sizeof(value));
    std::array<char, 33> buffer{};
    std::snprintf(
        buffer.data(), buffer.size(), "%08X%08X%08X%08X",
        value.a, value.b, value.c, value.d);
    output = buffer.data();
    return true;
}

bool read_player_guid(RC::Unreal::UObject* object, std::string& output)
{
    return read_guid_property(object, {STR("PlayerUId"), STR("PlayerUID")}, output);
}

bool read_nested_name_property(
    RC::Unreal::UObject* object,
    std::initializer_list<const TCHAR*> outer_names,
    std::initializer_list<const TCHAR*> inner_names,
    std::string& output)
{
    auto* outer_property = find_property(object, outer_names);
    auto* outer_struct_property =
        RC::Unreal::CastField<RC::Unreal::FStructProperty>(outer_property);
    auto* outer_struct = outer_struct_property ? outer_struct_property->GetStruct().Get() : nullptr;
    auto* outer_value = outer_property
                            ? outer_property->ContainerPtrToValuePtr<void>(object)
                            : nullptr;
    auto* inner_property = outer_struct
                               ? find_struct_property(outer_struct, inner_names)
                               : nullptr;
    auto* name_property = RC::Unreal::CastField<RC::Unreal::FNameProperty>(inner_property);
    if (!outer_value || !name_property) return false;
    const auto value = name_property->GetPropertyValueInContainer(outer_value);
    output = RC::to_utf8_string(value.ToString());
    return !output.empty();
}

bool read_string_property(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names, std::string& output)
{
    auto* property = find_property(object, names);
    auto* string_property = RC::Unreal::CastField<RC::Unreal::FStrProperty>(property);
    if (!string_property) return false;
    const auto& value = string_property->GetPropertyValueInContainer(object);
    output = RC::to_utf8_string(*value);
    return true;
}

bool read_account_name(RC::Unreal::UObject* object, std::string& output)
{
    return read_string_property(object, {STR("AccountName")}, output);
}

std::int32_t read_array_property_count(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    auto* property = find_property(object, names);
    auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
    if (!array_property) return -1;
    RC::Unreal::FScriptArrayHelper_InContainer values(array_property, object);
    const auto count = values.Num();
    return count >= 0 && count <= 100000 ? count : -1;
}

std::int32_t read_map_property_count(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names)
{
    auto* property = find_property(object, names);
    auto* map_property = RC::Unreal::CastField<RC::Unreal::FMapProperty>(property);
    if (!map_property) return -1;
    auto* values = static_cast<RC::Unreal::FScriptMap*>(
        property->ContainerPtrToValuePtr<void>(object));
    const auto count = values ? values->Num() : -1;
    return count >= 0 && count <= 100000 ? count : -1;
}

bool read_number_property(
    RC::Unreal::UObject* object, std::initializer_list<const TCHAR*> names, double& output)
{
    auto* property = find_property(object, names);
    if (!property) return false;
    if (auto* int_property = RC::Unreal::CastField<RC::Unreal::FIntProperty>(property)) {
        output = static_cast<double>(int_property->GetPropertyValueInContainer(object));
        return true;
    }
    if (auto* float_property = RC::Unreal::CastField<RC::Unreal::FFloatProperty>(property)) {
        output = static_cast<double>(float_property->GetPropertyValueInContainer(object));
        return true;
    }
    if (auto* double_property = RC::Unreal::CastField<RC::Unreal::FDoubleProperty>(property)) {
        output = double_property->GetPropertyValueInContainer(object);
        return true;
    }
    if (auto* byte_property = RC::Unreal::CastField<RC::Unreal::FByteProperty>(property)) {
        output = static_cast<double>(byte_property->GetPropertyValueInContainer(object));
        return true;
    }
    if (auto* int64_property = RC::Unreal::CastField<RC::Unreal::FInt64Property>(property)) {
        output = static_cast<double>(int64_property->GetPropertyValueInContainer(object));
        return true;
    }
    return false;
}

enum class PalWazaId : std::uint16_t
{
};

void read_pal_parameter_functions(RC::Unreal::UObject* parameter, PalSlotSnapshot& slot)
{
    if (!parameter || !RC::Unreal::UObject::IsReal(parameter)) return;
    try {
        if (auto* function = RC::Unreal::UObjectGlobals::StaticFindObject<RC::Unreal::UFunction*>(
                nullptr, nullptr, STR("/Script/Pal.PalIndividualCharacterParameter:GetCharacterID"))) {
            struct Params
            {
                RC::Unreal::FName ReturnValue{};
            };
            auto* return_property =
                RC::Unreal::CastField<RC::Unreal::FNameProperty>(function->GetReturnProperty());
            if (!return_property || function->GetParmsSize() != static_cast<std::int32_t>(sizeof(Params))) {
                return;
            }
            Params params;
            parameter->ProcessEvent(function, &params);
            slot.character_id = RC::to_utf8_string(params.ReturnValue.ToString());
        }
        if (auto* function = RC::Unreal::UObjectGlobals::StaticFindObject<RC::Unreal::UFunction*>(
                nullptr, nullptr, STR("/Script/Pal.PalIndividualCharacterParameter:GetLevel"))) {
            struct Params
            {
                std::int32_t ReturnValue{};
            };
            auto* return_property =
                RC::Unreal::CastField<RC::Unreal::FIntProperty>(function->GetReturnProperty());
            if (!return_property || function->GetParmsSize() != static_cast<std::int32_t>(sizeof(Params))) {
                return;
            }
            Params params;
            parameter->ProcessEvent(function, &params);
            if (params.ReturnValue >= 0 && params.ReturnValue <= 1000) {
                slot.level_found = true;
                slot.level = static_cast<double>(params.ReturnValue);
            }
        }
        if (auto* function = RC::Unreal::UObjectGlobals::StaticFindObject<RC::Unreal::UFunction*>(
                nullptr, nullptr, STR("/Script/Pal.PalIndividualCharacterParameter:GetPassiveSkillList"))) {
            struct Params
            {
                RC::Unreal::TArray<RC::Unreal::FName> ReturnValue{};
            };
            auto* return_property =
                RC::Unreal::CastField<RC::Unreal::FArrayProperty>(function->GetReturnProperty());
            auto* inner_property = return_property
                                       ? RC::Unreal::CastField<RC::Unreal::FNameProperty>(return_property->GetInner())
                                       : nullptr;
            if (!return_property || !inner_property ||
                function->GetParmsSize() != static_cast<std::int32_t>(sizeof(Params))) {
                return;
            }
            Params params;
            parameter->ProcessEvent(function, &params);
            const auto count = params.ReturnValue.Num();
            if (count >= 0 && count <= 16) {
                slot.passive_skill_ids.reserve(static_cast<size_t>(count));
                for (std::int32_t index = 0; index < count; ++index) {
                    slot.passive_skill_ids.emplace_back(
                        RC::to_utf8_string(params.ReturnValue[index].ToString()));
                }
            }
        }
        if (auto* function = RC::Unreal::UObjectGlobals::StaticFindObject<RC::Unreal::UFunction*>(
                nullptr, nullptr, STR("/Script/Pal.PalIndividualCharacterParameter:GetEquipWaza"))) {
            struct Params
            {
                RC::Unreal::TArray<PalWazaId> ReturnValue{};
            };
            auto* return_property =
                RC::Unreal::CastField<RC::Unreal::FArrayProperty>(function->GetReturnProperty());
            auto* inner_property = return_property ? return_property->GetInner() : nullptr;
            if (!return_property || !inner_property ||
                inner_property->GetSize() != static_cast<std::int32_t>(sizeof(PalWazaId)) ||
                function->GetParmsSize() != static_cast<std::int32_t>(sizeof(Params))) {
                return;
            }
            Params params;
            parameter->ProcessEvent(function, &params);
            const auto count = params.ReturnValue.Num();
            if (count >= 0 && count <= 16) {
                slot.equipped_waza_ids.reserve(static_cast<size_t>(count));
                for (std::int32_t index = 0; index < count; ++index) {
                    slot.equipped_waza_ids.emplace_back(
                        static_cast<std::uint16_t>(params.ReturnValue[index]));
                }
            }
        }
    } catch (...) {
    }
}

PalSlotArraySnapshot read_pal_slot_array(
    RC::Unreal::UObject* owner,
    std::initializer_list<const TCHAR*> property_names,
    std::vector<PropertyCandidateSnapshot>& slot_object_metadata,
    std::vector<PropertyCandidateSnapshot>& handle_metadata,
    std::vector<PropertyCandidateSnapshot>& parameter_metadata,
    std::vector<PropertyCandidateSnapshot>& handle_id_metadata,
    bool collect_metadata)
{
    PalSlotArraySnapshot snapshot;
    if (!owner || !RC::Unreal::UObject::IsReal(owner)) return snapshot;
    auto* property = find_property(owner, property_names);
    auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
    if (!array_property) return snapshot;
    RC::Unreal::FScriptArrayHelper_InContainer values(array_property, owner);
    const auto count = values.Num();
    if (count < 0 || count > 100000) return snapshot;
    snapshot.found = true;
    snapshot.slot_count = count;
    constexpr std::int32_t max_slots = 10;
    const auto limit = count < max_slots ? count : max_slots;
    auto* inner_object_property = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(array_property->GetInner());
    auto* slot_struct_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(array_property->GetInner());
    auto* slot_struct = slot_struct_property ? slot_struct_property->GetStruct().Get() : nullptr;
    for (std::int32_t index = 0; index < limit; ++index) {
        void* element = values.GetRawPtr(index);
        if (!element) continue;
        PalSlotSnapshot slot;
        slot.found = true;
        if (inner_object_property) {
            auto* slot_object = inner_object_property->GetObjectPropertyValue(element);
            if (slot_object && RC::Unreal::UObject::IsReal(slot_object)) {
                slot.slot_object_found = true;
                slot.slot_object = describe_object(slot_object);
                if (collect_metadata && index == 0) {
                    slot_object_metadata = collect_keyword_property_metadata(
                        slot_object,
                        {"individual", "handle", "slot", "id", "character", "order", "lock"});
                    handle_id_metadata = collect_struct_property_metadata(
                        slot_object, {STR("ReplicateHandleID")});
                }
                read_nested_guid_property(
                    slot_object,
                    {STR("ReplicateHandleID")},
                    {STR("PlayerUId"), STR("PlayerUID")},
                    slot.handle_player_uid);
                read_nested_guid_property(
                    slot_object,
                    {STR("ReplicateHandleID")},
                    {STR("InstanceId"), STR("InstanceID")},
                    slot.individual_id);
                slot.replicate_handle_id_hex =
                    slot.handle_player_uid + slot.individual_id;
                auto* handle = read_object_property(slot_object, {STR("Handle")});
                slot.handle_found = handle && RC::Unreal::UObject::IsReal(handle);
                if (slot.handle_found) {
                    slot.handle = describe_object(handle);
                    if (collect_metadata && index == 0) {
                        handle_metadata = collect_top_level_property_metadata(handle);
                    }
                }
                auto* parameter = read_object_property(
                    slot_object, {STR("ReplicateIndividualParameter")});
                slot.replicate_parameter_found =
                    parameter && RC::Unreal::UObject::IsReal(parameter);
                if (slot.replicate_parameter_found) {
                    slot.replicate_parameter = describe_object(parameter);
                    slot.level_found =
                        read_number_property(parameter, {STR("Level")}, slot.level);
                    slot.rank_found =
                        read_number_property(parameter, {STR("Rank")}, slot.rank);
                    slot.experience_found = read_number_property(
                        parameter,
                        {STR("Exp"), STR("Experience"), STR("RankUpExp")},
                        slot.experience);
                    slot.hp_found =
                        read_number_property(parameter, {STR("HP")}, slot.hp);
                    slot.max_hp_found =
                        read_number_property(parameter, {STR("MaxHP")}, slot.max_hp);
                    slot.full_stomach_found = read_number_property(
                        parameter, {STR("FullStomach")}, slot.full_stomach);
                    slot.sanity_found =
                        read_number_property(parameter, {STR("Sanity")}, slot.sanity);
                    read_string_property(
                        parameter, {STR("NickName"), STR("Nickname")}, slot.nickname);
                    read_pal_parameter_functions(parameter, slot);
                    if (collect_metadata && index == 0) {
                        parameter_metadata = collect_data_property_metadata(
                            parameter,
                            {"level", "rank", "exp", "hp", "health", "nickname", "name",
                             "character", "species", "pal", "passive", "skill", "waza",
                             "talent", "gender", "work", "status", "sanity", "stomach",
                             "hunger", "individual", "save", "basecamp", "favorite"});
                    }
                }
            }
        } else if (slot_struct) {
            auto* handle_property = find_struct_property(slot_struct, {STR("Handle")});
            auto* handle_object_property = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(handle_property);
            if (handle_property && handle_object_property) {
                auto* handle = handle_object_property->GetObjectPropertyValue(
                    handle_property->ContainerPtrToValuePtr<void>(element));
                slot.handle_found = handle && RC::Unreal::UObject::IsReal(handle);
                if (slot.handle_found) slot.handle = describe_object(handle);
            }
            auto* id_property = find_struct_property(slot_struct, {STR("IndividualId")});
            if (id_property && id_property->GetSize() >= static_cast<std::int32_t>(sizeof(PlayerGuid))) {
                PlayerGuid value{};
                std::memcpy(&value, id_property->ContainerPtrToValuePtr<void>(element), sizeof(value));
                std::array<char, 33> buffer{};
                std::snprintf(buffer.data(), buffer.size(), "%08X%08X%08X%08X", value.a, value.b, value.c, value.d);
                slot.individual_id = buffer.data();
            }
        }
        snapshot.slots.emplace_back(std::move(slot));
    }
    return snapshot;
}

std::vector<ItemSlotSnapshot> read_item_slots(
    RC::Unreal::UObject* container,
    bool collect_metadata,
    std::vector<PropertyCandidateSnapshot>& item_id_metadata)
{
    std::vector<ItemSlotSnapshot> slots;
    if (!container || !RC::Unreal::UObject::IsReal(container)) return slots;
    auto* property = find_property(container, {STR("ItemSlotArray")});
    auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
    auto* inner_object = array_property
                             ? RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(array_property->GetInner())
                             : nullptr;
    if (!array_property || !inner_object) return slots;
    RC::Unreal::FScriptArrayHelper_InContainer values(array_property, container);
    const auto count = values.Num();
    if (count < 0 || count > 100000) return slots;
    constexpr std::int32_t max_slots = 32;
    const auto limit = count < max_slots ? count : max_slots;
    for (std::int32_t index = 0; index < limit; ++index) {
        auto* element = values.GetRawPtr(index);
        auto* slot_object = element ? inner_object->GetObjectPropertyValue(element) : nullptr;
        if (!slot_object || !RC::Unreal::UObject::IsReal(slot_object)) continue;
        ItemSlotSnapshot slot;
        slot.found = true;
        slot.slot_index_found =
            read_number_property(slot_object, {STR("SlotIndex")}, slot.slot_index);
        slot.stack_count_found =
            read_number_property(slot_object, {STR("StackCount")}, slot.stack_count);
        read_nested_name_property(
            slot_object,
            {STR("ItemId"), STR("ItemID")},
            {STR("StaticId"), STR("StaticID")},
            slot.item_static_id);
        if (collect_metadata && item_id_metadata.empty()) {
            item_id_metadata = collect_struct_property_metadata(
                slot_object, {STR("ItemId"), STR("ItemID")});
        }
        slots.emplace_back(std::move(slot));
    }
    return slots;
}

std::vector<ItemContainerSnapshot> read_inventory_containers(
    RC::Unreal::UObject* inventory_helper,
    bool collect_metadata,
    std::vector<PropertyCandidateSnapshot>& container_metadata,
    std::vector<PropertyCandidateSnapshot>& slot_metadata,
    std::vector<PropertyCandidateSnapshot>& item_id_metadata)
{
    std::vector<ItemContainerSnapshot> containers;
    if (!inventory_helper || !RC::Unreal::UObject::IsReal(inventory_helper)) return containers;
    auto* property = find_property(inventory_helper, {STR("Containers")});
    auto* array_property = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
    auto* inner_object = array_property
                             ? RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(array_property->GetInner())
                             : nullptr;
    if (!array_property || !inner_object) return containers;
    RC::Unreal::FScriptArrayHelper_InContainer values(array_property, inventory_helper);
    const auto count = values.Num();
    if (count < 0 || count > 100000) return containers;
    constexpr std::int32_t max_containers = 16;
    const auto limit = count < max_containers ? count : max_containers;
    for (std::int32_t index = 0; index < limit; ++index) {
        auto* element = values.GetRawPtr(index);
        auto* container = element ? inner_object->GetObjectPropertyValue(element) : nullptr;
        if (!container || !RC::Unreal::UObject::IsReal(container)) continue;
        ItemContainerSnapshot snapshot;
        snapshot.found = true;
        snapshot.container = describe_object(container);
        snapshot.slot_count = read_array_property_count(
            container,
            {STR("ItemSlotArray"), STR("Slots"), STR("ItemSlots"), STR("SlotArray")});
        snapshot.slots = read_item_slots(container, collect_metadata, item_id_metadata);
        if (collect_metadata) {
            snapshot.container_property_metadata = collect_keyword_property_metadata(
                container,
                {"slot", "item", "container", "equipment", "loadout", "weapon", "armor"});
            if (container_metadata.empty()) {
                container_metadata = collect_top_level_property_metadata(container);
                slot_metadata = collect_array_element_metadata(
                    container,
                    {STR("ItemSlotArray"), STR("Slots"), STR("ItemSlots"), STR("SlotArray")});
            }
        }
        containers.emplace_back(std::move(snapshot));
    }
    return containers;
}

void read_cached_player_details(
    RC::Unreal::UObject* player_state, OnlinePlayerSnapshot& player, bool collect_metadata)
{
    auto* property = find_property(player_state, {STR("CachedPlayerLocation")});
    auto* struct_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(property);
    auto* structure = struct_property ? struct_property->GetStruct().Get() : nullptr;
    const auto type_name = structure ? RC::to_utf8_string(structure->GetFullName()) : std::string{};
    if (!property || !struct_property || !structure ||
        type_name != "ScriptStruct /Script/CoreUObject.Vector") {
        player.cached_location.error = "CachedPlayerLocation is unavailable";
    } else if (property->GetSize() != 12 && property->GetSize() != 24) {
        player.cached_location.error = "CachedPlayerLocation has an unsupported size";
    } else {
        const auto* raw = property->ContainerPtrToValuePtr<void>(player_state);
        if (!raw) {
            player.cached_location.error = "CachedPlayerLocation has no value address";
        } else if (property->GetSize() == 12) {
            std::array<float, 3> values{};
            std::memcpy(values.data(), raw, sizeof(values));
            for (size_t index = 0; index < values.size(); ++index) {
                player.cached_location.value[index] = static_cast<double>(values[index]);
            }
        } else {
            std::array<double, 3> values{};
            std::memcpy(values.data(), raw, sizeof(values));
            player.cached_location.value = values;
        }
        if (raw && std::all_of(player.cached_location.value.begin(), player.cached_location.value.end(),
                               [](const double value) { return std::isfinite(value); })) {
            player.cached_location.found = true;
        } else if (raw) {
            player.cached_location.value = {};
            player.cached_location.error = "CachedPlayerLocation contains non-finite values";
        }
    }

    auto* guild = read_object_property(player_state, {STR("GuildBelongTo")});
    player.guild_found = guild && RC::Unreal::UObject::IsReal(guild);
    if (player.guild_found) {
        player.guild = describe_object(guild);
        read_string_property(guild, {STR("GuildName"), STR("GroupName")}, player.guild_name);
        read_guid_property(guild, {STR("AdminPlayerUId")}, player.guild_admin_player_uid);
        player.base_camp_count = read_array_property_count(guild, {STR("BaseCampIds")});
        player.base_camp_level_found =
            read_number_property(guild, {STR("BaseCampLevel")}, player.base_camp_level);
    }

    auto* inventory = read_object_property(player_state, {STR("InventoryData")});
    player.inventory_found = inventory && RC::Unreal::UObject::IsReal(inventory);
    if (player.inventory_found) {
        player.inventory = describe_object(inventory);
        player.inventory_weight_found =
            read_number_property(inventory, {STR("NowItemWeight")}, player.now_item_weight) &&
            read_number_property(inventory, {STR("MaxInventoryWeight")}, player.max_inventory_weight);
        auto* inventory_helper = read_object_property(inventory, {STR("InventoryMultiHelper")});
        player.inventory_helper_found =
            inventory_helper && RC::Unreal::UObject::IsReal(inventory_helper);
        if (player.inventory_helper_found) {
            player.inventory_helper = describe_object(inventory_helper);
            player.inventory_container_count =
                read_array_property_count(inventory_helper, {STR("Containers")});
            player.inventory_containers = read_inventory_containers(
                inventory_helper,
                collect_metadata,
                player.inventory_container_property_metadata,
                player.inventory_slot_property_metadata,
                player.inventory_item_id_struct_metadata);
        }
    }

    auto* pal_storage = read_object_property(player_state, {STR("PalStorage")});
    player.pal_storage_found = pal_storage && RC::Unreal::UObject::IsReal(pal_storage);
    if (player.pal_storage_found) {
        player.pal_storage = describe_object(pal_storage);
        auto* pal_container = read_object_property(pal_storage, {STR("TargetContainer")});
        player.pal_container_found =
            pal_container && RC::Unreal::UObject::IsReal(pal_container);
        if (player.pal_container_found) {
            player.pal_container = describe_object(pal_container);
            const auto non_empty_count = read_array_property_count(
                pal_storage, {STR("CachedNonEmptySlots_InServer")});
            player.pal_slot_array = read_pal_slot_array(
                pal_container,
                {STR("SlotArray")},
                player.pal_slot_object_property_metadata,
                player.pal_handle_property_metadata,
                player.pal_parameter_property_metadata,
                player.pal_handle_id_struct_metadata,
                collect_metadata && non_empty_count <= 0);
            player.pal_non_empty_slot_array = read_pal_slot_array(
                pal_storage,
                {STR("CachedNonEmptySlots_InServer")},
                player.pal_slot_object_property_metadata,
                player.pal_handle_property_metadata,
                player.pal_parameter_property_metadata,
                player.pal_handle_id_struct_metadata,
                collect_metadata);
        }
    }

    auto* otomo = read_object_property(player_state, {STR("OtomoData")});
    player.otomo_found = otomo && RC::Unreal::UObject::IsReal(otomo);
    if (player.otomo_found) player.otomo = describe_object(otomo);

    player.player_data_ready =
        !player.player_uid.empty() &&
        player.player_uid != "00000000000000000000000000000000" &&
        player.inventory_found && player.pal_storage_found;

    if (collect_metadata) {
        player.detail_property_metadata_collected = true;
        if (player.guild_found) {
            player.guild_property_metadata = collect_keyword_property_metadata(
                guild,
                {"name", "guild", "group", "admin", "master", "member", "owner", "rank",
                 "base", "camp", "territory", "map"});
            player.base_camp_id_struct_metadata = collect_array_element_metadata(
                guild, {STR("BaseCampIds")});
        }
        if (player.inventory_found) {
            player.inventory_property_metadata = collect_keyword_property_metadata(
                inventory,
                {"container", "slot", "item", "equipment", "equip", "weapon", "armor",
                 "accessory", "storage", "weight"});
            player.inventory_all_property_metadata =
                collect_top_level_property_metadata(inventory);
            if (player.inventory_helper_found) {
                auto* inventory_helper = read_object_property(inventory, {STR("InventoryMultiHelper")});
                player.inventory_helper_property_metadata = collect_keyword_property_metadata(
                    inventory_helper,
                    {"container", "slot", "item", "equipment", "equip", "loadout", "inventory", "essential"});
            }
        }
        if (player.pal_storage_found) {
            player.pal_storage_property_metadata = collect_keyword_property_metadata(
                pal_storage,
                {"pal", "character", "container", "slot", "box", "storage", "individual"});
            if (player.pal_container_found) {
                auto* pal_container = read_object_property(pal_storage, {STR("TargetContainer")});
                player.pal_container_property_metadata = collect_keyword_property_metadata(
                    pal_container,
                    {"pal", "character", "slot", "handle", "container", "individual", "otomo"});
            }
        }
        if (player.otomo_found) {
            player.otomo_property_metadata = collect_keyword_property_metadata(
                otomo,
                {"pal", "character", "party", "slot", "team", "individual", "container"});
        }
    }
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

void append_game_state_player_states(
    RC::Unreal::UObject* world,
    std::vector<RC::Unreal::UObject*>& output,
    std::unordered_set<RC::Unreal::UObject*>& seen,
    bool& game_state_found,
    ObjectSnapshot& game_state_snapshot,
    bool& available,
    size_t& state_count,
    std::string& error)
{
    using namespace RC::Unreal;
    game_state_found = false;
    game_state_snapshot = {};
    available = false;
    state_count = 0;
    error.clear();
    try {
        if (!world || !UObject::IsReal(world)) {
            error = "World context is unavailable";
            return;
        }
        auto* game_state = read_object_property(world, {STR("GameState")});
        if (!game_state) {
            error = "World.GameState is unavailable";
            return;
        }
        game_state_found = true;
        game_state_snapshot = describe_object(game_state);
        auto* states_property = find_property(game_state, {STR("PlayerArray")});
        auto* states_array_property = CastField<FArrayProperty>(states_property);
        auto* inner_object_property = states_array_property
                                          ? CastField<FObjectPropertyBase>(states_array_property->GetInner())
                                          : nullptr;
        auto* player_state_class = inner_object_property
                                       ? inner_object_property->GetPropertyClass().Get()
                                       : nullptr;
        if (!states_property || !states_array_property || !player_state_class ||
            states_property->GetSize() < static_cast<std::int32_t>(sizeof(TArray<UObject*>))) {
            error = "GameState.PlayerArray does not match this game build";
            return;
        }
        FScriptArrayHelper_InContainer states(states_array_property, game_state);
        const auto count = states.Num();
        if (count < 0 || count > 1024) {
            error = "GameState.PlayerArray returned an invalid array size";
            return;
        }
        available = true;
        state_count = static_cast<size_t>(count);
        for (std::int32_t index = 0; index < count; ++index) {
            auto* state = inner_object_property->GetObjectPropertyValue(states.GetRawPtr(index));
            if (state && UObject::IsReal(state) && state->IsA(player_state_class) && seen.insert(state).second) {
                output.emplace_back(state);
            }
        }
    } catch (...) {
        error = "GameState.PlayerArray read failed";
    }
}

void populate_player_state(
    RC::Unreal::UObject* player_state, OnlinePlayerSnapshot& player,
    bool metadata_probe = false, bool collect_metadata = false)
{
    player.player_state_found = player_state != nullptr;
    if (!player_state) {
        player.identity_error = "PlayerState is unavailable";
        return;
    }
    player.player_state = describe_object(player_state);
    const auto uid_ok = read_player_guid(player_state, player.player_uid);
    const auto name_ok = read_account_name(player_state, player.account_name);
    read_cached_player_details(player_state, player, collect_metadata);
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

std::string china_time(unsigned long long milliseconds)
{
    if (milliseconds == 0) return {};
    constexpr std::time_t china_offset_seconds = 8 * 60 * 60;
    const auto seconds = static_cast<std::time_t>(milliseconds / 1000) + china_offset_seconds;
    std::tm china{};
    if (gmtime_s(&china, &seconds) != 0) return {};
    std::ostringstream output;
    output << std::put_time(&china, "%Y-%m-%dT%H:%M:%S") << '.' << std::setw(3) << std::setfill('0')
           << (milliseconds % 1000) << "+08:00";
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

std::string json_number(double value)
{
    std::ostringstream output;
    output << std::setprecision(15) << value;
    return output.str();
}

bool parse_json_string(std::string_view input, size_t& cursor, std::string& output)
{
    if (cursor >= input.size() || input[cursor++] != '"') return false;
    output.clear();
    while (cursor < input.size()) {
        const auto character = input[cursor++];
        if (character == '"') return true;
        if (character == '\\') {
            if (cursor >= input.size()) return false;
            const auto escaped = input[cursor++];
            if (escaped == '"' || escaped == '\\' || escaped == '/') output.push_back(escaped);
            else if (escaped == 'n') output.push_back('\n');
            else if (escaped == 'r') output.push_back('\r');
            else if (escaped == 't') output.push_back('\t');
            else return false;
        } else if (static_cast<unsigned char>(character) < 0x20) return false;
        else output.push_back(character);
    }
    return false;
}

void skip_json_space(std::string_view input, size_t& cursor)
{
    while (cursor < input.size() && std::isspace(static_cast<unsigned char>(input[cursor]))) ++cursor;
}

bool parse_json_object(std::string_view input, std::map<std::string, std::string>& values)
{
    size_t cursor = 0;
    skip_json_space(input, cursor);
    if (cursor >= input.size() || input[cursor++] != '{') return false;
    skip_json_space(input, cursor);
    if (cursor < input.size() && input[cursor] == '}') return true;
    while (cursor < input.size()) {
        std::string key;
        if (!parse_json_string(input, cursor, key)) return false;
        skip_json_space(input, cursor);
        if (cursor >= input.size() || input[cursor++] != ':') return false;
        skip_json_space(input, cursor);
        const auto value_start = cursor;
        if (cursor < input.size() && input[cursor] == '"') {
            std::string decoded;
            if (!parse_json_string(input, cursor, decoded)) return false;
            values[key] = "\"" + json_escape(decoded) + "\"";
        } else if (cursor < input.size() && input[cursor] == '[') {
            int depth = 0;
            bool in_string = false;
            while (cursor < input.size()) {
                const auto c = input[cursor++];
                if (c == '"' && (cursor < 2 || input[cursor - 2] != '\\')) in_string = !in_string;
                if (!in_string && c == '[') ++depth;
                if (!in_string && c == ']' && --depth == 0) break;
            }
            if (depth != 0 || in_string) return false;
            values[key] = std::string(input.substr(value_start, cursor - value_start));
        } else {
            while (cursor < input.size() && input[cursor] != ',' && input[cursor] != '}') ++cursor;
            auto raw = trim(std::string(input.substr(value_start, cursor - value_start)));
            if (raw.empty()) return false;
            values[key] = std::move(raw);
        }
        skip_json_space(input, cursor);
        if (cursor >= input.size()) return false;
        if (input[cursor] == '}') { ++cursor; skip_json_space(input, cursor); return cursor == input.size(); }
        if (input[cursor++] != ',') return false;
        skip_json_space(input, cursor);
    }
    return false;
}

bool json_bool(const std::map<std::string, std::string>& values, const char* key, bool& output)
{
    const auto found = values.find(key);
    if (found == values.end()) return false;
    if (found->second == "true") { output = true; return true; }
    if (found->second == "false") { output = false; return true; }
    return false;
}

bool json_string(const std::map<std::string, std::string>& values, const char* key, std::string& output)
{
    const auto found = values.find(key);
    if (found == values.end() || found->second.size() < 2 || found->second.front() != '"' || found->second.back() != '"') return false;
    output = found->second.substr(1, found->second.size() - 2);
    return !output.empty();
}

bool json_int(const std::map<std::string, std::string>& values, const char* key, std::int32_t& output)
{
    const auto found = values.find(key);
    if (found == values.end()) return false;
    try {
        size_t used = 0;
        const auto value = std::stoll(found->second, &used);
        if (used != found->second.size() || value < INT_MIN || value > INT_MAX) return false;
        output = static_cast<std::int32_t>(value);
        return true;
    } catch (...) { return false; }
}

bool normalize_guid(std::string& value)
{
    if (value.size() != 32) return false;
    for (auto& character : value) {
        if (!std::isxdigit(static_cast<unsigned char>(character))) return false;
        character = static_cast<char>(std::toupper(static_cast<unsigned char>(character)));
    }
    return true;
}

bool valid_name_id(const std::string& value)
{
    if (value.empty() || value.size() > 128) return false;
    return std::all_of(value.begin(), value.end(), [](unsigned char character) {
        return std::isalnum(character) || character == '_';
    });
}

bool parse_mutation_request(std::string_view input, MutationRequest& request, std::string& error)
{
    std::map<std::string, std::string> values;
    if (!parse_json_object(input, values)) { error = "invalid JSON object"; return false; }
    if (!json_string(values, "operation", request.operation) || !json_bool(values, "confirm", request.confirm) || !request.confirm) {
        error = "operation and confirm=true are required"; return false;
    }
    if (!json_string(values, "player_uid", request.player_uid) || !normalize_guid(request.player_uid)) {
        error = "player_uid must be a 32-digit hex GUID"; return false;
    }
    if (request.operation == "item_set_count") {
        if (!json_int(values, "container_index", request.container_index) || !json_int(values, "slot_index", request.slot_index) ||
            !json_int(values, "expected_stack_count", request.expected_stack_count) || !json_int(values, "stack_count", request.stack_count) ||
            !json_string(values, "expected_item_static_id", request.expected_item_static_id) ||
            request.container_index < 0 || request.slot_index < 0 || request.expected_stack_count < 0 || request.expected_stack_count > 9999 ||
            request.stack_count < 0 || request.stack_count > 9999 || !valid_name_id(request.expected_item_static_id)) {
            error = "invalid item_set_count fields"; return false;
        }
    } else if (request.operation == "pal_replace_passive") {
        if (!json_string(values, "instance_id", request.instance_id) || !json_string(values, "passive_skill_id", request.passive_skill_id) ||
            !json_string(values, "expected_character_id", request.expected_character_id) ||
            !json_bool(values, "add_passive", request.add_passive) || !normalize_guid(request.instance_id) ||
            !valid_name_id(request.passive_skill_id) || !valid_name_id(request.expected_character_id)) {
            error = "invalid pal_replace_passive fields"; return false;
        }
        const auto expected = values.find("expected_passive_skill_ids");
        if (expected == values.end() || expected->second.size() < 2 || expected->second.front() != '[' || expected->second.back() != ']') {
            error = "expected_passive_skill_ids is required"; return false;
        }
        std::string array = expected->second.substr(1, expected->second.size() - 2);
        size_t pos = 0;
        while (pos < array.size()) {
            while (pos < array.size() && std::isspace(static_cast<unsigned char>(array[pos]))) ++pos;
            if (pos >= array.size()) break;
            std::string item;
            if (!parse_json_string(array, pos, item)) { error = "invalid expected_passive_skill_ids"; return false; }
            if (!valid_name_id(item) || request.expected_passive_skill_ids.size() >= 16) {
                error = "invalid expected_passive_skill_ids"; return false;
            }
            request.expected_passive_skill_ids.push_back(std::move(item));
            while (pos < array.size() && std::isspace(static_cast<unsigned char>(array[pos]))) ++pos;
            if (pos < array.size() && array[pos++] != ',') { error = "invalid expected_passive_skill_ids"; return false; }
        }
    } else if (request.operation == "pal_set_stats") {
        if (!json_string(values, "instance_id", request.instance_id) || !normalize_guid(request.instance_id) ||
            !json_string(values, "expected_character_id", request.expected_character_id) ||
            !valid_name_id(request.expected_character_id) || !json_string(values, "field", request.value_field) ||
            !json_int(values, "expected_value", request.expected_value) || !json_int(values, "value", request.value) ||
            (request.value_field != "Level" && request.value_field != "Talent_HP" && request.value_field != "Talent_Shot" && request.value_field != "Talent_Defense") ||
            request.expected_value < 0 || request.value < 0 ||
            (request.value_field == "Level" ? request.expected_value < 1 || request.expected_value > 80 || request.value < 1 || request.value > 80 : request.expected_value > 100 || request.value > 100)) {
            error = "invalid pal_set_stats fields"; return false;
        }
    } else { error = "unsupported mutation operation"; return false; }
    return true;
}

void append_property_candidate_json(
    std::ostringstream& body, const PropertyCandidateSnapshot& candidate)
{
    body << "{\"name\":\"" << json_escape(candidate.name)
         << "\",\"kind\":\"" << json_escape(candidate.kind)
         << "\",\"declared_type\":\"" << json_escape(candidate.declared_type)
         << "\",\"object_value_found\":" << (candidate.object_value_found ? "true" : "false")
         << ",\"object_value\":{\"name\":\"" << json_escape(candidate.object_value.name)
         << "\",\"full_name\":\"" << json_escape(candidate.object_value.full_name)
         << "\",\"class_name\":\"" << json_escape(candidate.object_value.class_name)
         << "\"},\"collection_count_available\":"
         << (candidate.collection_count_available ? "true" : "false")
         << ",\"collection_count\":" << candidate.collection_count
         << ",\"nested_candidates\":[";
    for (size_t index = 0; index < candidate.nested_candidates.size(); ++index) {
        if (index > 0) body << ',';
        append_property_candidate_json(body, candidate.nested_candidates[index]);
    }
    body << "],\"function_candidates\":[";
    for (size_t index = 0; index < candidate.function_candidates.size(); ++index) {
        if (index > 0) body << ',';
        const auto& function = candidate.function_candidates[index];
        body << "{\"name\":\"" << json_escape(function.name)
             << "\",\"full_name\":\"" << json_escape(function.full_name)
             << "\",\"params_size\":" << function.params_size << ",\"parameters\":[";
        for (size_t parameter_index = 0; parameter_index < function.parameters.size(); ++parameter_index) {
            if (parameter_index > 0) body << ',';
            const auto& parameter = function.parameters[parameter_index];
            body << "{\"name\":\"" << json_escape(parameter.name)
                 << "\",\"kind\":\"" << json_escape(parameter.kind)
                 << "\",\"declared_type\":\"" << json_escape(parameter.declared_type)
                 << "\",\"size\":" << parameter.size
                 << ",\"return_value\":" << (parameter.return_value ? "true" : "false") << '}';
        }
        body << "]}";
    }
    body << "]}";
}

void append_property_metadata_json(
    std::ostringstream& body, const std::vector<PropertyCandidateSnapshot>& metadata)
{
    body << '[';
    for (size_t index = 0; index < metadata.size(); ++index) {
        if (index > 0) body << ',';
        const auto& candidate = metadata[index];
        body << "{\"name\":\"" << json_escape(candidate.name)
             << "\",\"kind\":\"" << json_escape(candidate.kind)
             << "\",\"declared_type\":\"" << json_escape(candidate.declared_type) << "\"}";
    }
    body << ']';
}

void append_pal_slot_array_json(std::ostringstream& body, const PalSlotArraySnapshot& slot_array)
{
    body << "{\"found\":" << (slot_array.found ? "true" : "false")
         << ",\"slot_count\":" << slot_array.slot_count << ",\"slots\":[";
    for (size_t index = 0; index < slot_array.slots.size(); ++index) {
        if (index > 0) body << ',';
        const auto& slot = slot_array.slots[index];
        body << "{\"found\":" << (slot.found ? "true" : "false")
             << ",\"individual_id\":\"" << json_escape(slot.individual_id)
             << "\",\"handle_player_uid\":\""
             << json_escape(slot.handle_player_uid)
             << "\",\"replicate_handle_id_hex\":\""
             << json_escape(slot.replicate_handle_id_hex)
             << "\",\"replicate_handle_id\":{\"player_uid\":\""
             << json_escape(slot.handle_player_uid)
             << "\",\"instance_id\":\"" << json_escape(slot.individual_id) << "\"}"
             << ",\"slot_object_found\":"
             << (slot.slot_object_found ? "true" : "false")
             << ",\"slot_object\":";
        if (slot.slot_object_found) {
            body << "{\"name\":\"" << json_escape(slot.slot_object.name)
                 << "\",\"full_name\":\"" << json_escape(slot.slot_object.full_name)
                 << "\",\"class_name\":\"" << json_escape(slot.slot_object.class_name) << "\"}";
        } else {
            body << "null";
        }
        body << ",\"handle_found\":" << (slot.handle_found ? "true" : "false")
             << ",\"handle\":";
        if (slot.handle_found) {
            body << "{\"name\":\"" << json_escape(slot.handle.name)
                 << "\",\"full_name\":\"" << json_escape(slot.handle.full_name)
                 << "\",\"class_name\":\"" << json_escape(slot.handle.class_name) << "\"}";
        } else {
            body << "null";
        }
        body << ",\"replicate_parameter_found\":"
             << (slot.replicate_parameter_found ? "true" : "false")
             << ",\"replicate_parameter\":";
        if (slot.replicate_parameter_found) {
            body << "{\"name\":\"" << json_escape(slot.replicate_parameter.name)
                 << "\",\"full_name\":\"" << json_escape(slot.replicate_parameter.full_name)
                 << "\",\"class_name\":\"" << json_escape(slot.replicate_parameter.class_name) << "\"}";
        } else {
            body << "null";
        }
        body << ",\"parameter_values\":{\"nickname\":\""
             << json_escape(slot.nickname)
             << "\",\"character_id\":\"" << json_escape(slot.character_id)
             << "\",\"level\":"
             << (slot.level_found ? json_number(slot.level) : std::string("null"))
             << ",\"rank\":"
             << (slot.rank_found ? json_number(slot.rank) : std::string("null"))
             << ",\"experience\":"
             << (slot.experience_found ? json_number(slot.experience) : std::string("null"))
             << ",\"hp\":"
             << (slot.hp_found ? json_number(slot.hp) : std::string("null"))
             << ",\"max_hp\":"
             << (slot.max_hp_found ? json_number(slot.max_hp) : std::string("null"))
             << ",\"full_stomach\":"
             << (slot.full_stomach_found ? json_number(slot.full_stomach) : std::string("null"))
             << ",\"sanity\":"
             << (slot.sanity_found ? json_number(slot.sanity) : std::string("null"))
             << ",\"passive_skill_ids\":[";
        for (size_t skill_index = 0; skill_index < slot.passive_skill_ids.size(); ++skill_index) {
            if (skill_index > 0) body << ',';
            body << '\"' << json_escape(slot.passive_skill_ids[skill_index]) << '\"';
        }
        body << "],\"equipped_waza_ids\":[";
        for (size_t skill_index = 0; skill_index < slot.equipped_waza_ids.size(); ++skill_index) {
            if (skill_index > 0) body << ',';
            body << slot.equipped_waza_ids[skill_index];
        }
        body << "]}";
        body << '}';
    }
    body << "]}";
}

void append_item_container_json(
    std::ostringstream& body, const ItemContainerSnapshot& container)
{
    body << "{\"found\":" << (container.found ? "true" : "false")
         << ",\"container\":";
    if (container.found) {
        body << "{\"name\":\"" << json_escape(container.container.name)
             << "\",\"full_name\":\"" << json_escape(container.container.full_name)
             << "\",\"class_name\":\"" << json_escape(container.container.class_name) << "\"}";
    } else {
        body << "null";
    }
    body << ",\"slot_count\":" << container.slot_count << ",\"slots\":[";
    for (size_t index = 0; index < container.slots.size(); ++index) {
        if (index > 0) body << ',';
        const auto& slot = container.slots[index];
        body << "{\"found\":" << (slot.found ? "true" : "false")
             << ",\"slot_index\":"
             << (slot.slot_index_found ? json_number(slot.slot_index) : std::string("null"))
             << ",\"stack_count\":"
             << (slot.stack_count_found ? json_number(slot.stack_count) : std::string("null"))
             << ",\"item_static_id\":\"" << json_escape(slot.item_static_id) << '\"'
             << '}';
    }
    body << "]}";
}

const char* job_kind_name(JobKind kind)
{
    if (kind == JobKind::World) return "world";
    if (kind == JobKind::OnlinePlayers) return "online_players";
    if (kind == JobKind::Mutation) return "mutation";
    return "game_thread";
}

std::string add_response_time(const std::string& body)
{
    if (body.empty() || body.front() != '{') return body;
    const auto now = unix_time_ms();
    std::ostringstream output;
    output << "{\"response_time_unix_ms\":" << now << ",\"response_time_china\":\""
           << china_time(now) << '"';
    if (body.size() > 2) output << ',' << body.substr(1);
    else output << '}';
    return output.str();
}

std::string response(int status, const std::string& body)
{
    const char* reason = status == 200 ? "OK" : status == 202 ? "Accepted" : status == 400 ? "Bad Request" : status == 401 ? "Unauthorized"
                                                                                           : status == 413 ? "Payload Too Large"
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

bool exact_noarg_function(RC::Unreal::UFunction* function)
{
    if (!function || function->GetReturnProperty()) return false;
    size_t count = 0;
    for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
             function, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
        if (property && property->HasAnyPropertyFlags(RC::Unreal::CPF_Parm)) ++count;
    }
    return count == 0;
}

bool exact_parameter_count(RC::Unreal::UFunction* function, size_t expected)
{
    if (!function) return false;
    size_t count = 0;
    for (auto* property : RC::Unreal::TFieldRange<RC::Unreal::FProperty>(
             function, RC::Unreal::EFieldIterationFlags::IncludeDeprecated)) {
        if (property && property->HasAnyPropertyFlags(RC::Unreal::CPF_Parm)) ++count;
    }
    return count == expected;
}

bool input_parameter(RC::Unreal::FProperty* property)
{
    using namespace RC::Unreal;
    return property && property->HasAnyPropertyFlags(CPF_Parm) &&
           !property->HasAnyPropertyFlags(CPF_ReturnParm) &&
           (!property->HasAnyPropertyFlags(CPF_OutParm) ||
            property->HasAnyPropertyFlags(CPF_ConstParm));
}

bool return_parameter(RC::Unreal::FProperty* property)
{
    using namespace RC::Unreal;
    return property && property->HasAnyPropertyFlags(CPF_Parm) &&
           property->HasAnyPropertyFlags(CPF_ReturnParm);
}

RC::Unreal::UObject* find_player_state_by_uid(const std::string& uid)
{
    std::vector<RC::Unreal::UObject*> states;
    std::unordered_set<RC::Unreal::UObject*> seen;
    append_instances("PalPlayerState", states, seen);
    append_instances("BP_PalPlayerState_C", states, seen);
    for (auto* state : states) {
        std::string actual;
        if (read_player_guid(state, actual) && actual == uid) return state;
    }
    return nullptr;
}

RC::Unreal::UObject* find_pal_parameter(
    RC::Unreal::UObject* player_state, const std::string& uid, const std::string& instance_id,
    std::string& error)
{
    auto* storage = read_object_property(player_state, {STR("PalStorage")});
    auto* container = read_object_property(storage, {STR("TargetContainer")});
    if (!storage || !container) { error = "PalStorage path is unavailable"; return nullptr; }
    const auto scan = [&](RC::Unreal::UObject* owner,
                          std::initializer_list<const TCHAR*> property_names)
        -> RC::Unreal::UObject* {
        auto* property = find_property(owner, property_names);
        auto* array = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(property);
        auto* inner = array
                          ? RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(
                                array->GetInner())
                          : nullptr;
        if (!array || !inner) return nullptr;
        RC::Unreal::FScriptArrayHelper_InContainer values(array, owner);
        if (values.Num() < 0 || values.Num() > 100000) return nullptr;
        for (std::int32_t index = 0; index < values.Num(); ++index) {
            auto* element = values.GetRawPtr(index);
            auto* slot = element ? inner->GetObjectPropertyValue(element) : nullptr;
            std::string slot_uid, slot_instance;
            if (!read_nested_guid_property(
                    slot,
                    {STR("ReplicateHandleID")},
                    {STR("PlayerUId"), STR("PlayerUID")},
                    slot_uid) ||
                !read_nested_guid_property(
                    slot,
                    {STR("ReplicateHandleID")},
                    {STR("InstanceId"), STR("InstanceID")},
                    slot_instance) ||
                slot_uid != uid || slot_instance != instance_id) {
                continue;
            }
            return read_object_property(slot, {STR("ReplicateIndividualParameter")});
        }
        return nullptr;
    };
    if (auto* parameter = scan(storage, {STR("CachedNonEmptySlots_InServer")})) {
        return parameter;
    }
    if (auto* parameter = scan(container, {STR("SlotArray")})) return parameter;
    error = "Pal instance_id was not found for player_uid";
    return nullptr;
}

std::string passive_json(const std::vector<std::string>& ids)
{
    std::ostringstream output;
    output << '[';
    for (size_t index = 0; index < ids.size(); ++index) {
        if (index) output << ',';
        output << '"' << json_escape(ids[index]) << '"';
    }
    output << ']';
    return output.str();
}

bool read_character_id_strict(RC::Unreal::UObject* parameter, std::string& output)
{
    if (!parameter || !RC::Unreal::UObject::IsReal(parameter)) return false;
    auto* function = parameter->GetFunctionByNameInChain(STR("GetCharacterID"));
    auto* return_property = function
                                ? RC::Unreal::CastField<RC::Unreal::FNameProperty>(
                                      function->GetReturnProperty())
                                : nullptr;
    if (!return_property || !exact_parameter_count(function, 1) ||
        !return_parameter(return_property) || function->GetParmsSize() !=
                                static_cast<std::int32_t>(sizeof(RC::Unreal::FName))) {
        return false;
    }
    struct Params
    {
        RC::Unreal::FName ReturnValue{};
    } params;
    parameter->ProcessEvent(function, &params);
    output = RC::to_utf8_string(params.ReturnValue.ToString());
    return valid_name_id(output);
}

bool read_passive_ids_strict(
    RC::Unreal::UObject* parameter, std::vector<std::string>& output)
{
    output.clear();
    if (!parameter || !RC::Unreal::UObject::IsReal(parameter)) return false;
    auto* function = parameter->GetFunctionByNameInChain(STR("GetPassiveSkillList"));
    auto* return_property = function
                                ? RC::Unreal::CastField<RC::Unreal::FArrayProperty>(
                                      function->GetReturnProperty())
                                : nullptr;
    auto* inner_property = return_property
                               ? RC::Unreal::CastField<RC::Unreal::FNameProperty>(
                                     return_property->GetInner())
                               : nullptr;
    struct Params
    {
        RC::Unreal::TArray<RC::Unreal::FName> ReturnValue{};
    } params;
    if (!return_property || !inner_property || !exact_parameter_count(function, 1) ||
        !return_parameter(return_property) ||
        function->GetParmsSize() != static_cast<std::int32_t>(sizeof(Params))) {
        return false;
    }
    parameter->ProcessEvent(function, &params);
    const auto count = params.ReturnValue.Num();
    if (count < 0 || count > 16) return false;
    output.reserve(static_cast<size_t>(count));
    for (std::int32_t index = 0; index < count; ++index) {
        auto id = RC::to_utf8_string(params.ReturnValue[index].ToString());
        if (!valid_name_id(id)) return false;
        output.emplace_back(std::move(id));
    }
    return true;
}

bool passive_call(RC::Unreal::UObject* parameter, const char* name, const std::string& id)
{
    if (!parameter) return false;
    auto* function = parameter->GetFunctionByNameInChain(name == std::string("AddPassiveSkill") ? STR("AddPassiveSkill") : STR("RemovePassiveSkill"));
    if (!function || function->GetReturnProperty() || !valid_name_id(id)) return false;
    auto* id_property = RC::Unreal::CastField<RC::Unreal::FNameProperty>(
        function->FindProperty(RC::Unreal::FName(STR("SkillId"), RC::Unreal::FNAME_Find)));
    auto* add_property = RC::Unreal::CastField<RC::Unreal::FNameProperty>(
        function->FindProperty(RC::Unreal::FName(STR("AddSkill"), RC::Unreal::FNAME_Find)));
    auto* override_property = RC::Unreal::CastField<RC::Unreal::FNameProperty>(
        function->FindProperty(RC::Unreal::FName(STR("OverrideSkill"), RC::Unreal::FNAME_Find)));
    if (name == std::string("AddPassiveSkill")) {
        if (!exact_parameter_count(function, 2) || !input_parameter(add_property) ||
            !input_parameter(override_property)) return false;
        if (function->GetParmsSize() <= 0 || function->GetParmsSize() > 4096) return false;
        std::vector<std::uint8_t> params(static_cast<size_t>(function->GetParmsSize()));
        const std::wstring wide(id.begin(), id.end());
        add_property->SetPropertyValueInContainer(params.data(), RC::Unreal::FName(wide.c_str()));
        override_property->SetPropertyValueInContainer(params.data(), RC::Unreal::FName{});
        parameter->ProcessEvent(function, params.data()); return true;
    }
    if (!exact_parameter_count(function, 1) || !input_parameter(id_property)) return false;
    if (function->GetParmsSize() <= 0 || function->GetParmsSize() > 4096) return false;
    std::vector<std::uint8_t> params(static_cast<size_t>(function->GetParmsSize()));
    const std::wstring wide(id.begin(), id.end());
    id_property->SetPropertyValueInContainer(params.data(), RC::Unreal::FName(wide.c_str()));
    parameter->ProcessEvent(function, params.data()); return true;
}

MutationResult execute_mutation(const MutationRequest& request)
{
    MutationResult result; result.operation = request.operation;
    auto* player_state = find_player_state_by_uid(request.player_uid);
    if (!player_state) { result.error = "online player_uid was not found"; return result; }
    if (request.operation == "item_set_count") {
        auto* inventory = read_object_property(player_state, {STR("InventoryData")});
        auto* helper = read_object_property(inventory, {STR("InventoryMultiHelper")});
        auto* containers_property = find_property(helper, {STR("Containers")});
        auto* containers_array = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(containers_property);
        if (!inventory || !helper || !containers_array) { result.error = "InventoryData->InventoryMultiHelper->Containers unavailable"; return result; }
        RC::Unreal::FScriptArrayHelper_InContainer containers(containers_array, helper);
        if (containers.Num() < 0 || containers.Num() > 64 || request.container_index >= containers.Num()) {
            result.error = "container_index out of range"; return result;
        }
        auto* container_element = containers.GetRawPtr(request.container_index);
        auto* container_inner = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(containers_array->GetInner());
        auto* container_object = container_element && container_inner ? container_inner->GetObjectPropertyValue(container_element) : nullptr;
        auto* slots_property = find_property(container_object, {STR("ItemSlotArray")});
        auto* slots_array = RC::Unreal::CastField<RC::Unreal::FArrayProperty>(slots_property);
        if (!container_object || !slots_array) { result.error = "ItemSlotArray unavailable"; return result; }
        RC::Unreal::FScriptArrayHelper_InContainer slots(slots_array, container_object);
        if (slots.Num() < 0 || slots.Num() > 10000 || request.slot_index >= slots.Num()) {
            result.error = "slot_index out of range"; return result;
        }
        auto* slot_element = slots.GetRawPtr(request.slot_index);
        auto* slot_inner_object = RC::Unreal::CastField<RC::Unreal::FObjectPropertyBase>(slots_array->GetInner());
        auto* slot_inner_struct = RC::Unreal::CastField<RC::Unreal::FStructProperty>(slots_array->GetInner());
        auto* slot_object = slot_element && slot_inner_object ? slot_inner_object->GetObjectPropertyValue(slot_element) : nullptr;
        auto* slot_container = slot_object ? static_cast<void*>(slot_object) : slot_element;
        auto* stack = slot_inner_struct ? find_struct_property(slot_inner_struct->GetStruct().Get(), {STR("StackCount")}) : find_property(slot_object, {STR("StackCount")});
        auto* stack_property = RC::Unreal::CastField<RC::Unreal::FIntProperty>(stack);
        if (!stack_property || !slot_container) { result.error = "StackCount is not FIntProperty"; return result; }
        std::string actual_item_static_id;
        if (!slot_object || !read_nested_name_property(
                                slot_object,
                                {STR("ItemId"), STR("ItemID")},
                                {STR("StaticId"), STR("StaticID")},
                                actual_item_static_id) ||
            actual_item_static_id != request.expected_item_static_id) {
            result.error = "expected_item_static_id mismatch"; return result;
        }
        const auto before = stack_property->GetPropertyValueInContainer(slot_container);
        result.before_json = std::to_string(before);
        if (before != request.expected_stack_count) { result.error = "expected_stack_count mismatch"; return result; }
        stack_property->SetPropertyValueInContainer(slot_container, request.stack_count);
        const auto after = stack_property->GetPropertyValueInContainer(slot_container);
        result.after_json = std::to_string(after);
        if (after == request.stack_count) { result.status = "succeeded"; result.rollback_status = "not_needed"; return result; }
        stack_property->SetPropertyValueInContainer(slot_container, before);
        const auto restored = stack_property->GetPropertyValueInContainer(slot_container);
        result.rollback_status = restored == before ? "succeeded" : "failed";
        result.error = "write verification failed"; result.status = restored == before ? "rolled_back" : "rollback_failed"; return result;
    }
    std::string error;
    auto* parameter = find_pal_parameter(player_state, request.player_uid, request.instance_id, error);
    if (!parameter) { result.error = error; return result; }
    std::string actual_character_id;
    if (!read_character_id_strict(parameter, actual_character_id) ||
        actual_character_id != request.expected_character_id) {
        result.error = "expected_character_id mismatch"; return result;
    }
    if (request.operation == "pal_replace_passive") {
        std::vector<std::string> original;
        if (!read_passive_ids_strict(parameter, original)) {
            result.error = "passive skill read ABI mismatch"; return result;
        }
        result.before_json = passive_json(original);
        if (original != request.expected_passive_skill_ids) {
            result.error = "expected_passive_skill_ids mismatch"; return result;
        }
        auto target = original;
        const auto found = std::find(target.begin(), target.end(), request.passive_skill_id);
        if ((request.add_passive && found != target.end()) ||
            (!request.add_passive && found == target.end())) {
            result.error = request.add_passive ? "passive skill already exists" : "passive skill does not exist";
            return result;
        }
        if (request.add_passive) target.emplace_back(request.passive_skill_id);
        else target.erase(found);
        if (!passive_call(
                parameter,
                request.add_passive ? "AddPassiveSkill" : "RemovePassiveSkill",
                request.passive_skill_id)) {
            result.error = "passive mutation ABI mismatch"; return result;
        }
        std::vector<std::string> after;
        const auto after_read = read_passive_ids_strict(parameter, after);
        if (after_read) result.after_json = passive_json(after);
        if (after_read && after == target) {
            result.status = "succeeded"; result.rollback_status = "not_needed"; return result;
        }
        const auto inverse_ok = passive_call(
            parameter,
            request.add_passive ? "RemovePassiveSkill" : "AddPassiveSkill",
            request.passive_skill_id);
        std::vector<std::string> restored;
        const auto restored_ok = inverse_ok && read_passive_ids_strict(parameter, restored) &&
                                 restored == original;
        result.rollback_status = restored_ok ? "succeeded" : "failed";
        if (!restored_ok) result.rollback_error = "inverse passive operation did not restore snapshot";
        result.status = restored_ok ? "rolled_back" : "rollback_failed";
        result.error = "passive write verification failed";
        return result;
    }
    auto* save_property = RC::Unreal::CastField<RC::Unreal::FStructProperty>(find_property(parameter, {STR("SaveParameter")}));
    auto* save_struct = save_property ? save_property->GetStruct().Get() : nullptr;
    auto* save_value = save_property ? save_property->ContainerPtrToValuePtr<void>(parameter) : nullptr;
    const std::wstring value_field_wide(request.value_field.begin(), request.value_field.end());
    auto* value_property = save_struct ? RC::Unreal::CastField<RC::Unreal::FByteProperty>(find_struct_property(save_struct, {value_field_wide.c_str()})) : nullptr;
    auto* on_rep = parameter->GetFunctionByNameInChain(STR("OnRep_SaveParameter"));
    if (!save_value || !value_property || !exact_noarg_function(on_rep)) { result.error = "SaveParameter byte ABI or OnRep_SaveParameter mismatch"; return result; }
    const auto before = static_cast<std::int32_t>(value_property->GetPropertyValueInContainer(save_value));
    result.before_json = std::to_string(before);
    if (before != request.expected_value) { result.error = "expected_value mismatch"; return result; }
    value_property->SetPropertyValueInContainer(save_value, static_cast<std::uint8_t>(request.value)); parameter->ProcessEvent(on_rep, nullptr);
    const auto after = static_cast<std::int32_t>(value_property->GetPropertyValueInContainer(save_value)); result.after_json = std::to_string(after);
    if (after == request.value) { result.status = "succeeded"; result.rollback_status = "not_needed"; return result; }
    value_property->SetPropertyValueInContainer(save_value, static_cast<std::uint8_t>(before)); parameter->ProcessEvent(on_rep, nullptr);
    const auto restored = static_cast<std::int32_t>(value_property->GetPropertyValueInContainer(save_value)); result.rollback_status = restored == before ? "succeeded" : "failed";
    result.status = restored == before ? "rolled_back" : "rollback_failed"; result.error = "stat write verification failed"; return result;
}
} // namespace

class PalPanelBridge final : public RC::CppUserModBase
{
  public:
    PalPanelBridge()
    {
        ModName = STR("PalPanelBridge");
        ModVersion = STR("0.1.35");
        ModDescription = STR("Authenticated localhost HTTP diagnostics and game-thread mutations");
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
        std::string job_id;
        Job job;
        {
            std::scoped_lock lock(jobs_mutex_);
            for (auto& [id, queued_job] : jobs_) {
                if (queued_job.status != "queued") continue;
                queued_job.status = "running";
                queued_job.executed_at_unix_ms = unix_time_ms();
                queued_job.game_thread_tick_count_at_execution =
                    game_thread_tick_count_.load(std::memory_order_relaxed);
                queued_job.unreal_initialized = unreal_initialized_.load();
                queued_job.game_thread_tick_seen = true;
                job_id = id;
                job = queued_job;
                break;
            }
        }
        if (job_id.empty()) return;

        if (job.kind == JobKind::Mutation) {
            try {
                job.mutation_result = execute_mutation(job.mutation_request);
                job.status = "completed";
                append_log(
                    "mutation operation=" + job.mutation_request.operation +
                    " player_uid=" + job.mutation_request.player_uid +
                    " status=" + job.mutation_result.status);
            }
            catch (...) { job.mutation_result.operation = job.mutation_request.operation; job.mutation_result.status = "failed"; job.mutation_result.error = "mutation exception"; job.status = "failed"; }
        } else if (job.kind == JobKind::World) {
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
                constexpr size_t metadata_player_limit = 1;
                for (auto* controller : controllers) {
                    if (!controller || job.online_players.size() >= max_results) continue;
                    OnlinePlayerSnapshot player;
                    player.source = "controller";
                    player.controller = describe_object(controller);
                    const auto collect_metadata = job.metadata_probe &&
                                                  job.metadata_player_count < metadata_player_limit;
                    auto* player_state = read_object_property(controller, {STR("PlayerState")});
                    if (player_state) seen_player_states.insert(player_state);
                    populate_player_state(player_state, player, job.metadata_probe, collect_metadata);
                    auto* pawn = read_object_property(controller, {STR("AcknowledgedPawn")});
                    if (!pawn) pawn = read_object_property(controller, {STR("Pawn")});
                    player.pawn_found = pawn != nullptr;
                    if (pawn) {
                        player.pawn = describe_object(pawn);
                        auto* character_parameter = read_object_property(
                            pawn, {STR("CharacterParameterComponent")});
                        player.character_parameter_found =
                            character_parameter && RC::Unreal::UObject::IsReal(character_parameter);
                        if (player.character_parameter_found) {
                            player.character_parameter = describe_object(character_parameter);
                            if (collect_metadata) {
                                player.character_parameter_property_metadata =
                                    collect_keyword_property_metadata(
                                        character_parameter,
                                        {"level", "exp", "experience", "rank", "status", "hp", "health", "parameter", "point"});
                            }
                        }
                        if (collect_metadata) {
                            player.pawn_property_metadata = collect_keyword_property_metadata(
                                pawn, {"inventory", "container", "equipment", "otomo", "party", "item", "slot", "pal"});
                        }
                    }
                    job.online_players.emplace_back(std::move(player));
                    if (collect_metadata) ++job.metadata_player_count;
                }
                std::vector<RC::Unreal::UObject*> player_states;
                std::unordered_set<RC::Unreal::UObject*> all_player_states;
                auto* world = RC::Unreal::UObjectGlobals::FindFirstOf(STR("World"));
                job.query_world_found = world && RC::Unreal::UObject::IsReal(world);
                if (job.query_world_found) job.query_world = describe_object(world);
                std::vector<RC::Unreal::UObject*> game_state_player_states;
                std::unordered_set<RC::Unreal::UObject*> game_state_seen;
                append_game_state_player_states(
                    world,
                    game_state_player_states,
                    game_state_seen,
                    job.game_state_found,
                    job.game_state,
                    job.game_state_player_array_available,
                    job.game_state_player_state_count,
                    job.game_state_error);
                for (auto* player_state : game_state_player_states) {
                    if (job.online_players.size() >= max_results) break;
                    if (!seen_player_states.insert(player_state).second) continue;
                    OnlinePlayerSnapshot player;
                    player.source = "game_state_player_array";
                    const auto collect_metadata = job.metadata_probe &&
                                                  job.metadata_player_count < metadata_player_limit;
                    populate_player_state(player_state, player, job.metadata_probe, collect_metadata);
                    job.online_players.emplace_back(std::move(player));
                    if (collect_metadata) ++job.metadata_player_count;
                }
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
                    const auto collect_metadata = job.metadata_probe &&
                                                  job.metadata_player_count < metadata_player_limit;
                    populate_player_state(player_state, player, job.metadata_probe, collect_metadata);
                    job.online_players.emplace_back(std::move(player));
                    if (collect_metadata) ++job.metadata_player_count;
                }
                append_instances("PalPlayerState", player_states, all_player_states);
                append_instances("BP_PalPlayerState_C", player_states, all_player_states);
                job.player_state_object_count = player_states.size();
                for (auto* player_state : player_states) {
                    if (job.online_players.size() >= max_results) break;
                    if (!seen_player_states.insert(player_state).second) continue;
                    OnlinePlayerSnapshot player;
                    player.source = "player_state_fallback";
                    const auto collect_metadata = job.metadata_probe &&
                                                  job.metadata_player_count < metadata_player_limit;
                    populate_player_state(player_state, player, job.metadata_probe, collect_metadata);
                    job.online_players.emplace_back(std::move(player));
                    if (collect_metadata) ++job.metadata_player_count;
                }
                job.metadata_truncated = job.metadata_probe && job.online_players.size() > metadata_player_limit;
            } catch (...) {
                job.status = "failed";
            }
        }
        if (job.status == "running") job.status = "completed";

        std::scoped_lock lock(jobs_mutex_);
        const auto found = jobs_.find(job_id);
        if (found != jobs_.end() && found->second.status == "running") {
            found->second = std::move(job);
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
        body << "{\"ok\":true,\"bridge_version\":\"0.1.35\",\"ue4ss_loaded\":true,"
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
        body << "{\"ok\":true,\"bridge_version\":\"0.1.35\"," << "\"unreal_initialized\":"
             << (unreal_initialized_.load() ? "true" : "false") << ','
             << "\"game_thread_tick_count\":" << game_thread_tick_count_.load(std::memory_order_relaxed) << ','
             << "\"last_game_thread_tick_unix_ms\":" << last_tick << ','
             << "\"last_game_thread_tick_age_ms\":" << (last_tick > 0 && now >= last_tick ? now - last_tick : 0) << ','
             << "\"bridge_uptime_ms\":" << (now >= started ? now - started : 0) << '}';
        return body.str();
    }

    std::string enqueue(JobKind kind, bool metadata_probe = false, MutationRequest mutation_request = {})
    {
        const auto now = std::chrono::duration_cast<std::chrono::milliseconds>(
                             std::chrono::system_clock::now().time_since_epoch())
                             .count();
        const auto prefix = kind == JobKind::World
                                ? "world_"
                                : kind == JobKind::OnlinePlayers ? "players_" : kind == JobKind::Mutation ? "mutation_" : "probe_";
        const auto id = std::string(prefix) + std::to_string(now) + "_" + std::to_string(++sequence_);
        std::scoped_lock lock(jobs_mutex_);
        if (jobs_.size() >= 64) {
            const auto removable = std::find_if(jobs_.begin(), jobs_.end(), [](const auto& entry) {
                return entry.second.status == "completed" || entry.second.status == "failed";
            });
            if (removable == jobs_.end()) return {};
            jobs_.erase(removable);
        }
        jobs_.emplace(id, Job{.id = id, .kind = kind, .queued_at_unix_ms = static_cast<unsigned long long>(now),
                              .metadata_probe = metadata_probe, .mutation_request = std::move(mutation_request)});
        return id;
    }

    std::string get_job(const std::string& id)
    {
        Job job;
        {
            std::scoped_lock lock(jobs_mutex_);
            const auto found = jobs_.find(id);
            if (found == jobs_.end()) return {};
            job = found->second;
        }
        std::ostringstream body;
        body << "{\"ok\":true,\"job\":{\"id\":\"" << job.id << "\",\"type\":\"" << job_kind_name(job.kind)
             << "\",\"status\":\"" << job.status << "\",\"queued_at_unix_ms\":" << job.queued_at_unix_ms
             << ",\"queued_at_china\":\"" << china_time(job.queued_at_unix_ms)
             << "\",\"executed_at_unix_ms\":" << job.executed_at_unix_ms
             << ",\"executed_at_china\":\"" << china_time(job.executed_at_unix_ms)
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
                  << ",\"game_state_found\":" << (job.game_state_found ? "true" : "false")
                  << ",\"game_state\":{\"name\":\"" << json_escape(job.game_state.name)
                  << "\",\"full_name\":\"" << json_escape(job.game_state.full_name)
                  << "\",\"class_name\":\"" << json_escape(job.game_state.class_name) << "\"}"
                  << ",\"game_state_player_array_available\":"
                  << (job.game_state_player_array_available ? "true" : "false")
                  << ",\"game_state_player_state_count\":" << job.game_state_player_state_count
                  << ",\"game_state_error\":\"" << json_escape(job.game_state_error) << '"'
                 << ",\"online_player_count\":" << job.online_players.size();
            if (job.metadata_probe) {
                body << ",\"metadata_probe\":true,\"metadata_player_limit\":1,\"metadata_player_count\":"
                     << job.metadata_player_count << ",\"metadata_truncated\":"
                     << (job.metadata_truncated ? "true" : "false");
            }
            body << ",\"players\":[";
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
                      << json_escape(player.identity_error)
                      << "\",\"player_data_ready\":"
                      << (player.player_data_ready ? "true" : "false")
                      << ",\"player_data_state\":\""
                      << (player.player_data_ready ? "ready" : "initializing")
                      << "\",\"pawn_found\":"
                     << (player.pawn_found ? "true" : "false") << ",\"pawn\":{\"name\":\""
                      << json_escape(player.pawn.name) << "\",\"full_name\":\""
                      << json_escape(player.pawn.full_name) << "\",\"class_name\":\""
                      << json_escape(player.pawn.class_name) << "\"},\"cached_location_found\":"
                      << (player.cached_location.found ? "true" : "false") << ",\"cached_location\":";
                 if (player.cached_location.found) {
                     body << "{\"x\":" << player.cached_location.value[0]
                          << ",\"y\":" << player.cached_location.value[1]
                          << ",\"z\":" << player.cached_location.value[2] << '}';
                 } else {
                     body << "null";
                 }
                 body << ",\"cached_location_error\":\""
                      << json_escape(player.cached_location.error) << "\",\"guild_found\":"
                      << (player.guild_found ? "true" : "false") << ",\"guild\":";
                 if (player.guild_found) {
                     body << "{\"name\":\"" << json_escape(player.guild.name)
                          << "\",\"full_name\":\"" << json_escape(player.guild.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.guild.class_name) << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"guild_name\":\"" << json_escape(player.guild_name)
                      << "\",\"guild_admin_player_uid\":\""
                      << json_escape(player.guild_admin_player_uid)
                      << "\",\"base_camp_count\":" << player.base_camp_count
                      << ",\"base_camp_level_found\":"
                      << (player.base_camp_level_found ? "true" : "false")
                      << ",\"base_camp_level\":"
                      << (player.base_camp_level_found
                              ? json_number(player.base_camp_level)
                              : std::string("null"))
                      << ",\"inventory_found\":"
                      << (player.inventory_found ? "true" : "false") << ",\"inventory\":";
                 if (player.inventory_found) {
                     body << "{\"name\":\"" << json_escape(player.inventory.name)
                          << "\",\"full_name\":\"" << json_escape(player.inventory.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.inventory.class_name)
                          << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"inventory_container_count\":"
                      << player.inventory_container_count
                      << ",\"inventory_helper_found\":"
                      << (player.inventory_helper_found ? "true" : "false")
                      << ",\"inventory_helper\":";
                 if (player.inventory_helper_found) {
                     body << "{\"name\":\"" << json_escape(player.inventory_helper.name)
                          << "\",\"full_name\":\"" << json_escape(player.inventory_helper.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.inventory_helper.class_name)
                          << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"inventory_weight_found\":"
                      << (player.inventory_weight_found ? "true" : "false")
                      << ",\"now_item_weight\":"
                      << (player.inventory_weight_found
                              ? json_number(player.now_item_weight)
                              : std::string("null"))
                      << ",\"max_inventory_weight\":"
                      << (player.inventory_weight_found
                              ? json_number(player.max_inventory_weight)
                              : std::string("null"))
                      << ",\"pal_storage_found\":"
                      << (player.pal_storage_found ? "true" : "false") << ",\"pal_storage\":";
                 if (player.pal_storage_found) {
                     body << "{\"name\":\"" << json_escape(player.pal_storage.name)
                          << "\",\"full_name\":\"" << json_escape(player.pal_storage.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.pal_storage.class_name)
                          << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"pal_container_found\":"
                      << (player.pal_container_found ? "true" : "false") << ",\"pal_container\":";
                 if (player.pal_container_found) {
                     body << "{\"name\":\"" << json_escape(player.pal_container.name)
                          << "\",\"full_name\":\"" << json_escape(player.pal_container.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.pal_container.class_name)
                          << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"otomo_found\":"
                      << (player.otomo_found ? "true" : "false") << ",\"otomo\":";
                 if (player.otomo_found) {
                     body << "{\"name\":\"" << json_escape(player.otomo.name)
                          << "\",\"full_name\":\"" << json_escape(player.otomo.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.otomo.class_name)
                          << "\"}";
                 } else {
                     body << "null";
                 }
                 body << ",\"character_parameter_found\":"
                      << (player.character_parameter_found ? "true" : "false")
                      << ",\"character_parameter\":";
                 if (player.character_parameter_found) {
                     body << "{\"name\":\"" << json_escape(player.character_parameter.name)
                          << "\",\"full_name\":\"" << json_escape(player.character_parameter.full_name)
                          << "\",\"class_name\":\"" << json_escape(player.character_parameter.class_name) << "\"}";
                 } else {
                     body << "null";
                 }
                  body << ",\"pal_slot_array\":";
                  append_pal_slot_array_json(body, player.pal_slot_array);
                  body << ",\"pal_non_empty_slot_array\":";
                  append_pal_slot_array_json(body, player.pal_non_empty_slot_array);
                 body << ",\"inventory_containers\":[";
                 for (size_t container_index = 0; container_index < player.inventory_containers.size(); ++container_index) {
                     if (container_index > 0) body << ',';
                     append_item_container_json(body, player.inventory_containers[container_index]);
                 }
                 body << ']';
                if (job.metadata_probe) {
                    if (player.detail_property_metadata_collected) {
                        body << ",\"detail_property_metadata\":{\"guild\":";
                        append_property_metadata_json(body, player.guild_property_metadata);
                        body << ",\"inventory\":";
                        append_property_metadata_json(body, player.inventory_property_metadata);
                        body << ",\"inventory_all\":";
                        append_property_metadata_json(body, player.inventory_all_property_metadata);
                        body << ",\"inventory_helper\":";
                        append_property_metadata_json(body, player.inventory_helper_property_metadata);
                        body << ",\"inventory_container\":";
                        append_property_metadata_json(body, player.inventory_container_property_metadata);
                        body << ",\"inventory_slot\":";
                        append_property_metadata_json(body, player.inventory_slot_property_metadata);
                        body << ",\"inventory_item_id_struct\":";
                        append_property_metadata_json(body, player.inventory_item_id_struct_metadata);
                        body << ",\"pal_storage\":";
                        append_property_metadata_json(body, player.pal_storage_property_metadata);
                        body << ",\"pal_container\":";
                        append_property_metadata_json(body, player.pal_container_property_metadata);
                        body << ",\"pal_slot_object\":";
                        append_property_metadata_json(body, player.pal_slot_object_property_metadata);
                        body << ",\"pal_handle\":";
                        append_property_metadata_json(body, player.pal_handle_property_metadata);
                        body << ",\"pal_parameter\":";
                        append_property_metadata_json(body, player.pal_parameter_property_metadata);
                        body << ",\"pal_handle_id_struct\":";
                        append_property_metadata_json(body, player.pal_handle_id_struct_metadata);
                        body << ",\"base_camp_id_struct\":";
                        append_property_metadata_json(body, player.base_camp_id_struct_metadata);
                        body << ",\"otomo\":";
                        append_property_metadata_json(body, player.otomo_property_metadata);
                        body << ",\"character_parameter\":";
                        append_property_metadata_json(body, player.character_parameter_property_metadata);
                        body << '}';
                    }
                }
                body << '}';
            }
            body << ']';
        } else if (job.kind == JobKind::Mutation) {
            const auto& mutation = job.mutation_result;
            body << ",\"operation\":\"" << json_escape(mutation.operation)
                 << "\",\"mutation_status\":\"" << json_escape(mutation.status)
                 << "\",\"before\":" << mutation.before_json
                 << ",\"after\":" << mutation.after_json
                 << ",\"error\":\"" << json_escape(mutation.error)
                 << "\",\"rollback_status\":\"" << json_escape(mutation.rollback_status)
                 << "\",\"rollback_error\":\"" << json_escape(mutation.rollback_error) << '"';
        }
        body << "}}}";
        return body.str();
    }

    void handle(SOCKET client)
    {
        DWORD timeout = 2000;
        setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, reinterpret_cast<const char*>(&timeout), sizeof(timeout));
        std::string request;
        char buffer[4096]{};
        size_t header_end = std::string::npos;
        size_t content_length = 0;
        bool payload_too_large = false;
        for (;;) {
            const auto received = recv(client, buffer, sizeof(buffer), 0);
            if (received <= 0) return;
            request.append(buffer, static_cast<size_t>(received));
            if (request.size() > 32768) return;
            header_end = request.find("\r\n\r\n");
            if (header_end != std::string::npos) {
                const auto header = request.substr(0, header_end);
                auto lower_header = header;
                std::transform(
                    lower_header.begin(), lower_header.end(), lower_header.begin(),
                    [](unsigned char character) {
                        return static_cast<char>(std::tolower(character));
                    });
                const auto marker = lower_header.find("\r\ncontent-length:");
                if (marker != std::string::npos) {
                    const auto start = marker + 17;
                    const auto end = header.find("\r\n", start);
                    try { content_length = std::stoul(trim(header.substr(start, end - start))); }
                    catch (...) { return; }
                }
                if (content_length > 16384) {
                    payload_too_large = true;
                    break;
                }
                if (request.size() >= header_end + 4 + content_length) break;
            }
        }
        const auto first_end = request.find("\r\n");
        if (first_end == std::string::npos) return;
        const auto first = request.substr(0, first_end);
        const auto body_start = header_end == std::string::npos ? request.size() : header_end + 4;
        const auto request_body = body_start <= request.size() ? request.substr(body_start, content_length) : std::string{};

        int status = 404;
        std::string body{"{\"ok\":false,\"error\":{\"code\":\"not_found\",\"message\":\"route not found\"}}"};
        if (payload_too_large) {
            status = 413;
            body = "{\"ok\":false,\"error\":{\"code\":\"payload_too_large\",\"message\":\"request body exceeds 16384 bytes\"}}";
        } else if (config_.token.empty()) {
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
            status = id.empty() ? 503 : 202;
            body = id.empty()
                       ? "{\"ok\":false,\"error\":{\"code\":\"job_queue_full\",\"message\":\"all job slots are queued or running\"}}"
                       : "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/players/online HTTP/1.1") {
            const auto id = enqueue(JobKind::OnlinePlayers);
            status = id.empty() ? 503 : 202;
            body = id.empty()
                       ? "{\"ok\":false,\"error\":{\"code\":\"job_queue_full\",\"message\":\"all job slots are queued or running\"}}"
                       : "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/players/online/metadata HTTP/1.1") {
            const auto id = enqueue(JobKind::OnlinePlayers, true);
            status = id.empty() ? 503 : 202;
            body = id.empty()
                       ? "{\"ok\":false,\"error\":{\"code\":\"job_queue_full\",\"message\":\"all job slots are queued or running\"}}"
                       : "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/probe/game-thread HTTP/1.1") {
            const auto id = enqueue(JobKind::GameThread);
            status = id.empty() ? 503 : 202;
            body = id.empty()
                       ? "{\"ok\":false,\"error\":{\"code\":\"job_queue_full\",\"message\":\"all job slots are queued or running\"}}"
                       : "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
        } else if (first == "POST /v1/mutations HTTP/1.1") {
            MutationRequest mutation;
            std::string error;
            if (!parse_mutation_request(request_body, mutation, error)) {
                status = 400;
                body = "{\"ok\":false,\"error\":{\"code\":\"invalid_mutation\",\"message\":\"" + json_escape(error) + "\"}}";
            } else {
                const auto id = enqueue(JobKind::Mutation, false, std::move(mutation));
                status = id.empty() ? 503 : 202;
                body = id.empty()
                           ? "{\"ok\":false,\"error\":{\"code\":\"job_queue_full\",\"message\":\"all job slots are queued or running\"}}"
                           : "{\"ok\":true,\"job_id\":\"" + id + "\",\"status\":\"queued\"}";
            }
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
        size_t sent_total = 0;
        while (sent_total < wire.size()) {
            const auto remaining = wire.size() - sent_total;
            const auto chunk_size = remaining > static_cast<size_t>(INT_MAX)
                                        ? INT_MAX
                                        : static_cast<int>(remaining);
            const auto sent = send(client, wire.data() + sent_total, chunk_size, 0);
            if (sent == SOCKET_ERROR || sent == 0) {
                append_log("HTTP response send failed error=" + std::to_string(WSAGetLastError()) +
                           " sent=" + std::to_string(sent_total) +
                           " total=" + std::to_string(wire.size()));
                break;
            }
            sent_total += static_cast<size_t>(sent);
        }
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
