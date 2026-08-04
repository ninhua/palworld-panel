// Generated from docs/openapi.yaml. Do not edit.
export interface components {
  schemas: {
    "AITranslationConfig": {
      "api_key_present": boolean;
      "base_url": string;
      "configured": boolean;
      "custom_header_names": Array<string>;
      "model": string;
      "proxy_configured": boolean;
      "proxy_url": string;
      "timeout_seconds": number;
    };
    "AITranslationConfigEnvelope": {
      "data": components["schemas"]["AITranslationConfig"];
      "ok": true;
    };
    "AITranslationConfigUpdate": {
      "api_key"?: string;
      "base_url"?: string;
      "clear_api_key"?: boolean;
      "clear_custom_headers"?: boolean;
      "clear_proxy"?: boolean;
      "custom_headers"?: Record<string, string>;
      "model"?: string;
      "proxy_url"?: string;
      "timeout_seconds"?: number;
    };
    "AITranslationTestEnvelope": {
      "data": components["schemas"]["AITranslationTestResult"];
      "ok": true;
    };
    "AITranslationTestResult": {
      "base_url": string;
      "custom_header_names": Array<string>;
      "message": string;
      "model": string;
      "ok": boolean;
      "proxy_configured": boolean;
      "timeout_seconds": number;
    };
    "AstrBotCommunityServerRequest": {
      "country"?: string;
      "limit"?: number;
      "query"?: string;
    };
    "AstrBotControlRequest": {
      "action": "start" | "safe_stop" | "safe_restart" | "force_stop";
      "actor_qq_id": string;
      "group_id"?: string;
      "message"?: string;
      "waittime"?: number;
    };
    "AstrBotOnlinePlayer": {
      "level"?: number;
      "name": string;
    };
    "AstrBotServerStatus": {
      "info"?: {
        "server_name"?: string;
        "version"?: string;
      };
      "online_count": number;
      "online_players": Array<components["schemas"]["AstrBotOnlinePlayer"]>;
      "players_available"?: boolean;
      "server": {
        "container": {
          "exists": boolean;
          "status": string;
        };
        "pending_restart": boolean;
        "runtime_mode": string;
        "setup_step": string;
      };
    };
    "AstrBotServerStatusEnvelope": {
      "data": components["schemas"]["AstrBotServerStatus"];
      "ok": true;
    };
    "AuthCredentials": {
      "password": string;
      "username": string;
    };
    "AuthStatus": {
      "authenticated": boolean;
      "initialized": boolean;
      "user"?: components["schemas"]["SessionInfo"];
    };
    "AuthStatusEnvelope": {
      "data": components["schemas"]["AuthStatus"];
      "ok": true;
    };
    "BaseFeedBox": {
      "container_id": string;
      "container_name": string;
      "container_type": string;
      "slots": Array<components["schemas"]["SaveInventorySlot"]>;
    };
    "BaseFeedBoxSummary": {
      "box_count": number;
      "empty_box_count": number;
      "item_types": number;
      "occupied_slots": number;
      "total_items": number;
    };
    "BaseFeedBoxesEnvelope": {
      "data": components["schemas"]["BaseFeedBoxesResult"];
      "ok": true;
    };
    "BaseFeedBoxesResult": {
      "base": components["schemas"]["BaseWorkerBase"];
      "feed_boxes": Array<components["schemas"]["BaseFeedBox"]>;
      "items": Array<components["schemas"]["BaseFeedItemSummary"]>;
      "source_id": string;
      "status": components["schemas"]["SaveIndexStatus"];
      "summary": components["schemas"]["BaseFeedBoxSummary"];
    };
    "BaseFeedItemSummary": {
      "box_count": number;
      "count": number;
      "item_icon": string;
      "item_id": string;
      "item_name": string;
    };
    "BaseWorkerBase": {
      "custom_name": string;
      "guild_id": string;
      "guild_name": string;
      "has_custom_name": boolean;
      "id": string;
      "name": string;
      "raw_name": string;
      "x": number;
      "y": number;
      "z": number;
    };
    "BaseWorkerDetail": {
      "character_id": string;
      "gender": string;
      "instance_id": string;
      "level": number;
      "location_type": string;
      "name": string;
      "nickname": string;
      "on_expedition": boolean;
      "passives": Array<string>;
      "rank": number;
      "raw_passives": Array<string>;
      "species_name": string;
      "status": string;
    };
    "BaseWorkerSummary": {
      "average_level": number;
      "max_level": number;
      "named_count": number;
      "species_count": number;
      "total": number;
    };
    "BaseWorkersEnvelope": {
      "data": components["schemas"]["BaseWorkersResult"];
      "ok": true;
    };
    "BaseWorkersResult": {
      "base": components["schemas"]["BaseWorkerBase"];
      "source_id": string;
      "status": components["schemas"]["SaveIndexStatus"];
      "summary": components["schemas"]["BaseWorkerSummary"];
      "workers": Array<components["schemas"]["BaseWorkerDetail"]>;
    };
    "BossCreateSummonRequest": {
      "location_override"?: components["schemas"]["BossLocation"];
      "metadata"?: components["schemas"]["JsonObject"];
      "notes"?: string;
      "request_key": string;
      "template_id": string;
    };
    "BossExecutionAttempt": {
      "actor"?: string;
      "adapter": string;
      "command_count": number;
      "commands": Array<string>;
      "completed_at"?: string;
      "completed_commands": number;
      "created_at": string;
      "details": components["schemas"]["JsonObject"];
      "failure"?: string;
      "id": string;
      "responses": Array<string>;
      "started_at": string;
      "status": "running" | "succeeded" | "failed" | "uncertain";
      "summon_id": string;
      "updated_at": string;
      "wave_position": number;
    };
    "BossExecutionCapabilities": {
      "custom_pal_template": boolean;
      "exact_multipliers": boolean;
      "fixed_coordinates": boolean;
      "multiple_spawns": boolean;
      "uncapturable": boolean;
    };
    "BossExecutionStatus": {
      "active_waves": number;
      "adapter": string;
      "available": boolean;
      "busy": boolean;
      "capabilities": components["schemas"]["BossExecutionCapabilities"];
      "limitations": Array<string>;
      "message"?: string;
      "reconciliation_required": boolean;
      "running_attempts": number;
      "state": string;
      "uncertain_attempts": number;
    };
    "BossLocation": {
      "label"?: string;
      "x": number;
      "y": number;
      "z": number;
    };
    "BossReward": {
      "archived_at"?: string;
      "created_at": string;
      "description"?: string;
      "enabled": boolean;
      "id": string;
      "items": Array<components["schemas"]["BossRewardItem"]>;
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_templates": Array<string>;
      "points": number;
      "updated_at": string;
    };
    "BossRewardInput": {
      "description"?: string;
      "enabled": boolean;
      "items": Array<components["schemas"]["BossRewardItem"]>;
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_templates": Array<string>;
      "points": number;
    };
    "BossRewardItem": {
      "count": number;
      "item_id": string;
    };
    "BossRunDueResult": {
      "checked": number;
      "failed": number;
      "skipped": number;
      "summons": number;
      "warnings": number;
    };
    "BossSchedule": {
      "archived_at"?: string;
      "created_at": string;
      "cron"?: string;
      "daily_time"?: string;
      "enabled": boolean;
      "id": string;
      "last_run_at"?: string;
      "last_warning_at"?: string;
      "location_override"?: components["schemas"]["BossLocation"];
      "metadata": components["schemas"]["JsonObject"];
      "mode": "daily" | "cron";
      "name": string;
      "next_run_at"?: string;
      "template_id": string;
      "template_name": string;
      "timezone": string;
      "updated_at": string;
      "warning_message"?: string;
      "warning_minutes": number;
      "warning_title"?: string;
    };
    "BossScheduleEvent": {
      "actor"?: string;
      "created_at": string;
      "details": components["schemas"]["JsonObject"];
      "event_type": "warning" | "summon";
      "id": number;
      "message"?: string;
      "planned_for": string;
      "schedule_id": string;
      "status": "success" | "failed" | "skipped";
      "summon_id"?: string;
    };
    "BossScheduleInput": {
      "cron": string;
      "daily_time": string;
      "enabled": boolean;
      "location_override"?: components["schemas"]["BossLocation"];
      "metadata": components["schemas"]["JsonObject"];
      "mode": "daily" | "cron";
      "name": string;
      "template_id": string;
      "timezone": string;
      "warning_message": string;
      "warning_minutes": number;
      "warning_title": string;
    };
    "BossSummary": {
      "active_summons": number;
      "active_waves": number;
      "cancelled_summons": number;
      "completed_summons": number;
      "completed_waves": number;
      "enabled_rewards": number;
      "enabled_schedules": number;
      "enabled_templates": number;
      "failed_summons": number;
      "failed_waves": number;
      "pending_summons": number;
      "pending_waves": number;
      "rewards": number;
      "schedules": number;
      "skipped_waves": number;
      "template_waves": number;
      "templates": number;
    };
    "BossSummon": {
      "actor"?: string;
      "attack_multiplier": number;
      "capturable": boolean;
      "completed_at"?: string;
      "count": number;
      "defense_multiplier": number;
      "execution_mode": "record_only";
      "failure"?: string;
      "hp_multiplier": number;
      "id": string;
      "level": number;
      "location": components["schemas"]["BossLocation"];
      "metadata": components["schemas"]["JsonObject"];
      "notes"?: string;
      "pal_id": string;
      "request_key": string;
      "requested_at": string;
      "result": components["schemas"]["JsonObject"];
      "reward_id"?: string;
      "spawn_radius": number;
      "started_at"?: string;
      "status": "pending" | "active" | "completed" | "failed" | "cancelled";
      "template_id": string;
      "template_name": string;
      "updated_at": string;
    };
    "BossSummonEvent": {
      "actor"?: string;
      "created_at": string;
      "details": components["schemas"]["JsonObject"];
      "from_status"?: string;
      "id": number;
      "message"?: string;
      "summon_id": string;
      "to_status": "pending" | "active" | "completed" | "failed" | "cancelled";
    };
    "BossSummonResult": {
      "duplicate": boolean;
      "summon": components["schemas"]["BossSummon"];
    };
    "BossSummonWave": {
      "actor"?: string;
      "attack_multiplier": number;
      "capturable": boolean;
      "completed_at"?: string;
      "count": number;
      "created_at": string;
      "defense_multiplier": number;
      "delay_seconds": number;
      "failure"?: string;
      "hp_multiplier": number;
      "id": number;
      "kind": "main" | "minion" | "reinforcement";
      "level": number;
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_id": string;
      "position": number;
      "result": components["schemas"]["JsonObject"];
      "source_wave_id"?: string;
      "spawn_radius": number;
      "started_at"?: string;
      "status": "pending" | "active" | "completed" | "failed" | "skipped";
      "summon_id": string;
      "updated_at": string;
    };
    "BossTemplate": {
      "archived_at"?: string;
      "attack_multiplier": number;
      "capturable": boolean;
      "cooldown_seconds": number;
      "count": number;
      "created_at": string;
      "defense_multiplier": number;
      "description"?: string;
      "enabled": boolean;
      "hp_multiplier": number;
      "id": string;
      "level": number;
      "location": components["schemas"]["BossLocation"];
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_id": string;
      "reward_id"?: string;
      "spawn_radius": number;
      "updated_at": string;
    };
    "BossTemplateInput": {
      "attack_multiplier": number;
      "capturable": boolean;
      "cooldown_seconds": number;
      "count": number;
      "defense_multiplier": number;
      "description"?: string;
      "enabled": boolean;
      "hp_multiplier": number;
      "level": number;
      "location": components["schemas"]["BossLocation"];
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_id": string;
      "reward_id": string;
      "spawn_radius": number;
    };
    "BossTransitionRequest": {
      "message"?: string;
      "result"?: components["schemas"]["JsonObject"];
      "status": "pending" | "active" | "completed" | "failed" | "cancelled";
    };
    "BossWave": {
      "attack_multiplier": number;
      "capturable": boolean;
      "count": number;
      "created_at": string;
      "defense_multiplier": number;
      "delay_seconds": number;
      "hp_multiplier": number;
      "id": string;
      "kind": "main" | "minion" | "reinforcement";
      "level": number;
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_id": string;
      "position": number;
      "spawn_radius": number;
      "template_id": string;
      "updated_at": string;
    };
    "BossWaveInput": {
      "attack_multiplier": number;
      "capturable": boolean;
      "count": number;
      "defense_multiplier": number;
      "delay_seconds": number;
      "hp_multiplier": number;
      "kind": "main" | "minion" | "reinforcement";
      "level": number;
      "metadata": components["schemas"]["JsonObject"];
      "name": string;
      "pal_id": string;
      "spawn_radius": number;
    };
    "BossWaveSetRequest": {
      "waves": Array<components["schemas"]["BossWaveInput"]>;
    };
    "BossWaveTransitionRequest": {
      "message"?: string;
      "result"?: components["schemas"]["JsonObject"];
      "status": "active" | "completed" | "failed" | "skipped";
    };
    "BreedingStatus": {
      "available": boolean;
      "checked_at"?: string;
      "configured": boolean;
      "database_version"?: string;
      "last_error"?: string;
      "latency_ms": number;
      "upstream_version"?: string;
    };
    "BreedingStatusEnvelope": {
      "data": components["schemas"]["BreedingStatus"];
      "ok": true;
    };
    "CommunityServer": {
      "address": string;
      "connect": string;
      "country": string;
      "description"?: string;
      "id": string;
      "max_players": number;
      "name": string;
      "password": boolean;
      "players": number;
      "port": number;
      "status": "online" | "offline";
      "updated_at"?: string;
      "version"?: string;
    };
    "CommunityServerResult": {
      "cache_age_seconds": number;
      "fetched_at": string;
      "page": number;
      "page_size": number;
      "servers": Array<components["schemas"]["CommunityServer"]>;
      "source": "battlemetrics";
      "source_total": number;
      "stale": boolean;
      "total": number;
    };
    "CommunityServerResultEnvelope": {
      "data": components["schemas"]["CommunityServerResult"];
      "ok": true;
    };
    "CommunityServerSourceStatus": {
      "base_url": string;
      "cache_available": boolean;
      "cache_error"?: string;
      "cache_fresh": boolean;
      "cache_writable": boolean;
      "cached_queries": number;
      "enabled": boolean;
      "last_attempt_at"?: string;
      "last_error"?: string;
      "last_success_at"?: string;
      "next_refresh_at"?: string;
      "proxy_configured": boolean;
      "rate_limit_per_minute": number;
      "reachable": boolean;
      "source": "battlemetrics";
    };
    "CommunityServerSourceStatusEnvelope": {
      "data": components["schemas"]["CommunityServerSourceStatus"];
      "ok": true;
    };
    "CrashGuardEvent": {
      "created_at": string;
      "exit_code": number;
      "finished_at"?: string;
      "id": string;
      "kind": "unexpected_exit" | "container_restart" | "oom_kill";
      "message": string;
      "occurrences": number;
      "oom_killed": boolean;
      "restart_count": number;
      "runtime_mode": string;
      "started_at"?: string;
    };
    "CrashGuardRecoveryRequest": {
      "confirm": true;
      "start"?: boolean;
    };
    "CrashGuardStatus": {
      "enabled": true;
      "events": Array<components["schemas"]["CrashGuardEvent"]>;
      "expected_operation": boolean;
      "last_observed_runtime"?: string;
      "last_observed_status"?: string;
      "reason"?: string;
      "recent_crash_count": number;
      "threshold": number;
      "tripped": boolean;
      "tripped_at"?: string;
      "updated_at": string;
      "window_seconds": number;
    };
    "CrashGuardStatusEnvelope": {
      "data": components["schemas"]["CrashGuardStatus"];
      "ok": true;
    };
    "DevelopmentKey": {
      "created_at": string;
      "id": string;
      "last_used_at"?: string;
      "name": string;
      "prefix": string;
      "revoked_at"?: string;
      "token"?: string;
    };
    "DevelopmentKeyEnvelope": {
      "data": components["schemas"]["DevelopmentKey"];
      "ok": true;
    };
    "DevelopmentKeyInput": {
      "name": string;
    };
    "DevelopmentKeyListEnvelope": {
      "data": Array<components["schemas"]["DevelopmentKey"]>;
      "ok": true;
    };
    "DiagnosticHTTPRequest": {
      "body"?: string;
      "headers"?: Record<string, string>;
      "method": "GET" | "HEAD" | "POST" | "PUT" | "PATCH" | "DELETE";
      "url": string;
    };
    "DiagnosticHTTPResult": {
      "body": string;
      "duration_ms": number;
      "headers": Record<string, Array<string>>;
      "method": string;
      "status": string;
      "status_code": number;
      "truncated": boolean;
      "url": string;
    };
    "DiagnosticHTTPResultEnvelope": {
      "data": components["schemas"]["DiagnosticHTTPResult"];
      "ok": true;
    };
    "DiagnosticShellRequest": {
      "command": string;
      "confirm": true;
    };
    "DiagnosticShellResult": {
      "command": string;
      "duration_ms": number;
      "error": string;
      "exit_code": number;
      "output": string;
      "success": boolean;
      "timed_out": boolean;
      "truncated": boolean;
    };
    "DiagnosticShellResultEnvelope": {
      "data": components["schemas"]["DiagnosticShellResult"];
      "ok": true;
    };
    "DiagnosticStatus": {
      "http_enabled": boolean;
      "max_output": number;
      "platform": string;
      "shell_enabled": boolean;
      "timeout_ms": number;
    };
    "DiagnosticStatusEnvelope": {
      "data": components["schemas"]["DiagnosticStatus"];
      "ok": true;
    };
    "ErrorEnvelope": {
      "error": {
        "code": string;
        "message": string;
      };
      "ok": false;
    };
    "GlobalInventoryItem": {
      "category": string;
      "item_icon": string;
      "item_id": string;
      "item_name": string;
      "locations": Array<components["schemas"]["GlobalInventoryLocation"]>;
      "total_count": number;
    };
    "GlobalInventoryLocation": {
      "container_id": string;
      "container_name": string;
      "container_type": string;
      "count": number;
      "guild_name"?: string;
      "owner_id": string;
      "owner_name": string;
      "owner_type": "base" | "player" | "unknown";
      "slot": number;
    };
    "GlobalInventoryResult": {
      "filters": {
        "categories": Array<string>;
        "owner_types": Array<"all" | "base" | "player" | "unknown">;
      };
      "items": Array<components["schemas"]["GlobalInventoryItem"]>;
      "source_id": string;
      "status": components["schemas"]["SaveIndexStatus"];
      "summary": components["schemas"]["GlobalInventorySummary"];
    };
    "GlobalInventorySummary": {
      "container_count": number;
      "item_types": number;
      "limit": number;
      "location_count": number;
      "offset": number;
      "returned": number;
      "total_count": number;
      "unresolved_containers": number;
    };
    "Guild": {
      "base_ids": Array<string>;
      "id": string;
      "members": Array<components["schemas"]["GuildMemberSummary"]>;
      "name": string;
      "online_member_count": number;
      "owner_player_uid": string;
    };
    "GuildBaseDetail": {
      "custom_name": string;
      "has_custom_name": boolean;
      "id": string;
      "name": string;
      "raw_name": string;
      "status": string;
      "structures_count": number;
      "workers_count": number;
      "x": number;
      "y": number;
      "z": number;
    };
    "GuildDetailEnvelope": {
      "data": components["schemas"]["GuildDetailResult"];
      "ok": true;
    };
    "GuildDetailResult": {
      "bases": Array<components["schemas"]["GuildBaseDetail"]>;
      "guild": components["schemas"]["Guild"];
      "members": Array<components["schemas"]["GuildMemberDetail"]>;
      "source_id": string;
      "status": components["schemas"]["SaveIndexStatus"];
    };
    "GuildMemberDetail": {
      "annotation_updated_at"?: string;
      "has_annotation": boolean;
      "id": string;
      "is_online": boolean;
      "is_owner": boolean;
      "last_online_time": string;
      "level": number;
      "nickname": string;
      "note": string;
      "player_uid": string;
      "steam_id": string;
      "tags": Array<string>;
    };
    "GuildMemberSummary": {
      "last_online_time"?: string;
      "nickname": string;
      "player_uid": string;
    };
    "HostMigrationExecuteRequest": {
      "confirm": true;
      "migration_source_id": string;
      "name"?: string;
      "steam_id": string;
    };
    "HostMigrationPlan": {
      "can_execute": boolean;
      "source_dps_exists": boolean;
      "source_player_file": string;
      "source_uid": string;
      "steam_id": string;
      "strategy": "direct" | "target_exists" | "already_migrated";
      "target_dps_exists": boolean;
      "target_player_exists": boolean;
      "target_uid": string;
      "warnings": Array<string>;
    };
    "HostMigrationPlanEnvelope": {
      "data": components["schemas"]["HostMigrationPlan"];
      "ok": true;
    };
    "HostMigrationRequest": {
      "migration_source_id": string;
      "steam_id": string;
    };
    "HostMigrationResult": {
      "plan": components["schemas"]["HostMigrationPlan"];
      "source": components["schemas"]["SaveSource"];
      "verification": Record<string, unknown>;
    };
    "HostMigrationResultEnvelope": {
      "data": components["schemas"]["HostMigrationResult"];
      "ok": true;
    };
    "ImportCandidate": {
      "action": "new" | "update" | "unknown";
      "existing_mod_id"?: string;
      "file_name"?: string;
      "file_size"?: number;
      "id": string;
      "name"?: string;
      "package_name"?: string;
      "ready": boolean;
      "source_type": "workshop" | "github_asset" | "https_zip" | "local_zip";
      "version"?: string;
      "warnings"?: Array<string>;
    };
    "ImportInspection": {
      "candidates": Array<components["schemas"]["ImportCandidate"]>;
      "expires_at": string;
      "id": string;
      "selected_candidate_id"?: string;
      "source": string;
      "source_type": "workshop" | "github_release" | "https_zip" | "local_zip";
    };
    "ImportInspectionEnvelope": {
      "data": components["schemas"]["ImportInspection"];
      "ok": true;
    };
    "Incident": {
      "acknowledged_at"?: string;
      "details"?: Record<string, unknown>;
      "first_seen_at": string;
      "id": string;
      "kind": string;
      "last_seen_at": string;
      "occurrences": number;
      "resolved_at"?: string;
      "severity": components["schemas"]["IncidentSeverity"];
      "source": string;
      "status": components["schemas"]["IncidentStatus"];
      "summary": string;
      "title": string;
      "updated_at": string;
    };
    "IncidentActionEnvelope": {
      "data": components["schemas"]["IncidentActionResult"];
      "ok": true;
    };
    "IncidentActionRequest": {
      "confirm": true;
      "message"?: string;
    };
    "IncidentActionResult": {
      "event": components["schemas"]["IncidentEvent"];
      "incident": components["schemas"]["Incident"];
    };
    "IncidentDelivery": {
      "attempts": number;
      "created_at": string;
      "delivered_at"?: string;
      "event_id": string;
      "id": string;
      "incident_id": string;
      "next_attempt_at"?: string;
      "status": "pending" | "delivered" | "failed";
      "updated_at": string;
    };
    "IncidentDetail": {
      "deliveries": Array<components["schemas"]["IncidentDelivery"]>;
      "events": Array<components["schemas"]["IncidentEvent"]>;
      "incident": components["schemas"]["Incident"];
    };
    "IncidentDetailEnvelope": {
      "data": components["schemas"]["IncidentDetail"];
      "ok": true;
    };
    "IncidentEvent": {
      "actor"?: string;
      "created_at": string;
      "id": string;
      "incident_id": string;
      "message"?: string;
      "type": "opened" | "occurred" | "reopened" | "acknowledged" | "resolved" | "delivery_failed";
    };
    "IncidentListEnvelope": {
      "data": components["schemas"]["IncidentListResult"];
      "ok": true;
    };
    "IncidentListResult": {
      "items": Array<components["schemas"]["Incident"]>;
      "limit": number;
      "offset": number;
      "summary": components["schemas"]["IncidentSummary"];
      "total": number;
      "webhook": components["schemas"]["IncidentWebhookStatus"];
    };
    "IncidentSeverity": "info" | "warning" | "error" | "critical";
    "IncidentStatus": "open" | "acknowledged" | "resolved";
    "IncidentSummary": {
      "acknowledged": number;
      "open": number;
      "resolved": number;
    };
    "IncidentWebhookStatus": {
      "enabled": boolean;
      "max_attempts": number;
      "signed": boolean;
      "target_host"?: string;
      "timeout_seconds": number;
    };
    "IncidentWebhookTestEnvelope": {
      "data": components["schemas"]["IncidentWebhookTestResult"];
      "ok": true;
    };
    "IncidentWebhookTestRequest": {
      "confirm": true;
    };
    "IncidentWebhookTestResult": {
      "delivered": true;
      "target_host": string;
    };
    "Job": {
      "created_at": string;
      "error"?: string;
      "error_code"?: string;
      "id": string;
      "message": string;
      "progress": number;
      "status": "queued" | "waiting" | "running" | "completed" | "failed";
      "type": string;
      "updated_at": string;
    };
    "JobEnvelope": {
      "data": components["schemas"]["Job"];
      "ok": true;
    };
    "JsonObject": Record<string, unknown>;
    "ListSummary": {
      "limit": number;
      "offset": number;
      "page": number;
      "returned": number;
      "total": number;
    };
    "LocalModActionCapability": {
      "action": "import" | "repair" | "ignore" | "unignore" | "delete";
      "available": boolean;
      "confirmation_required": boolean;
      "reason"?: string;
    };
    "LocalModActionEnvelope": {
      "data": components["schemas"]["LocalModActionResult"];
      "ok": true;
    };
    "LocalModActionRequest": {
      "action": "import" | "repair" | "ignore" | "unignore" | "delete";
      "confirm"?: boolean;
      "revision": string;
    };
    "LocalModActionResult": {
      "action": "import" | "repair" | "ignore" | "unignore" | "delete";
      "finding_id": string;
      "message": string;
      "mod"?: components["schemas"]["ModRecord"];
      "scan": components["schemas"]["LocalScanResult"];
    };
    "LocalModFinding": {
      "actions": Array<components["schemas"]["LocalModActionCapability"]>;
      "classifications": Array<"managed" | "manual" | "present" | "missing_files" | "unknown" | "disabled" | "duplicate" | "incomplete">;
      "confidence": "high" | "medium" | "low";
      "database_mods"?: Array<components["schemas"]["ModRecord"]>;
      "duplicate": boolean;
      "enabled": boolean;
      "id": string;
      "ignored": boolean;
      "issues"?: Array<string>;
      "name": string;
      "ownership": "managed" | "manual";
      "package_name"?: string;
      "paths": Array<string>;
      "revision": string;
      "source": "workshop" | "legacy_pak" | "ue4ss" | "database";
      "state": "present" | "missing_files" | "unknown" | "disabled" | "duplicate" | "incomplete";
      "version"?: string;
    };
    "LocalScanEnvelope": {
      "data": components["schemas"]["LocalScanResult"];
      "ok": true;
    };
    "LocalScanResult": {
      "findings": Array<components["schemas"]["LocalModFinding"]>;
      "scanned_at": string;
      "server_dir": string;
      "skipped_paths": Array<string>;
      "warnings": Array<string>;
    };
    "ModConfigBackup": {
      "created_at": string;
      "id": string;
      "revision": string;
      "size": number;
    };
    "ModConfigBackupListEnvelope": {
      "data": Array<components["schemas"]["ModConfigBackup"]>;
      "ok": true;
    };
    "ModConfigDocument": {
      "content": string;
      "fields"?: Array<components["schemas"]["ModConfigurationField"]>;
      "file": components["schemas"]["ModConfigFile"];
      "format": string;
    };
    "ModConfigDocumentEnvelope": {
      "data": components["schemas"]["ModConfigDocument"];
      "ok": true;
    };
    "ModConfigFile": {
      "executable": boolean;
      "extension": ".json" | ".ini" | ".cfg" | ".toml" | ".yaml" | ".yml" | ".txt" | ".lua";
      "id": string;
      "modified_at": string;
      "name": string;
      "path": string;
      "revision": string;
      "risk"?: string;
      "size": number;
    };
    "ModConfigFileListEnvelope": {
      "data": Array<components["schemas"]["ModConfigFile"]>;
      "ok": true;
    };
    "ModConfigRestoreRequest": {
      "revision": string;
    };
    "ModConfigWriteRequest": {
      "confirm_executable"?: boolean;
      "content": string;
      "revision": string;
    };
    "ModConfigurationAdapter": {
      "available": boolean;
      "description": string;
      "files": Array<components["schemas"]["ModConfigFile"]>;
      "id": string;
      "name": string;
      "reload_behavior": "online_reload" | "restart_required";
      "workshop_id"?: string;
    };
    "ModConfigurationAdapterListEnvelope": {
      "data": Array<components["schemas"]["ModConfigurationAdapter"]>;
      "ok": true;
    };
    "ModConfigurationField": {
      "label": string;
      "max"?: number;
      "min"?: number;
      "path": string;
      "type": "boolean" | "integer" | "number" | "string";
      "value": unknown;
    };
    "ModImportInspectRequest": {
      "source": string;
    };
    "ModImportRequest": {
      "candidate_id"?: string;
      "inspection_id": string;
    };
    "ModImportSelectRequest": {
      "candidate_id": string;
    };
    "ModImportUploadRequest": {
      "file": string;
    };
    "ModRecord": {
      "created_at": string;
      "enabled": boolean;
      "file_size"?: number;
      "id": string;
      "last_checked_at"?: string;
      "name": string;
      "package_name": string;
      "path": string;
      "preview_url"?: string;
      "source": string;
      "steam_url"?: string;
      "subscriptions"?: number;
      "summary"?: string;
      "tags"?: Array<string>;
      "time_updated"?: number;
      "updated_at": string;
      "version"?: string;
      "workshop_id"?: string;
    };
    "MonitorHistoryEnvelope": {
      "data": Array<components["schemas"]["MonitorSample"]>;
      "ok": true;
    };
    "MonitorRiskReason": {
      "code": "host_memory_pressure" | "swap_exhaustion" | "workload_memory_pressure" | "oom_killed" | "abnormal_exit";
      "message": string;
      "severity": "warning" | "critical";
    };
    "MonitorSample": {
      "cpu_available": boolean;
      "cpu_percent": number;
      "created_at": string;
      "current_players": number;
      "disk_available": boolean;
      "disk_free_bytes": number;
      "disk_total_bytes": number;
      "exit_code": number;
      "finished_at"?: string;
      "game_port_healthy": boolean;
      "host_memory_available": boolean;
      "host_memory_available_bytes": number;
      "host_memory_total_bytes": number;
      "host_swap_free_bytes": number;
      "host_swap_total_bytes": number;
      "id": string;
      "lifecycle_available": boolean;
      "max_players": number;
      "memory_available": boolean;
      "memory_limit_bytes": number;
      "memory_usage_bytes": number;
      "oom_killed": boolean;
      "query_port_healthy": boolean;
      "rcon_healthy": boolean;
      "rest_healthy": boolean;
      "restart_count": number;
      "risk_reasons": Array<components["schemas"]["MonitorRiskReason"]>;
      "started_at"?: string;
      "unavailable_reason"?: string;
      "workload_memory_available": boolean;
      "workload_memory_limit_bytes": number;
      "workload_memory_usage_bytes": number;
    };
    "MonitorSnapshot": {
      "sample": components["schemas"]["MonitorSample"];
    };
    "MonitorSnapshotEnvelope": {
      "data": components["schemas"]["MonitorSnapshot"];
      "ok": true;
    };
    "NetworkProxyConfig": {
      "community": components["schemas"]["NetworkProxyEndpoint"];
      "install": components["schemas"]["NetworkProxyEndpoint"];
    };
    "NetworkProxyConfigEnvelope": {
      "data": components["schemas"]["NetworkProxyConfig"];
      "ok": true;
    };
    "NetworkProxyConfigUpdate": {
      "clear_community_proxy"?: boolean;
      "clear_install_proxy"?: boolean;
      "community_enabled"?: boolean;
      "community_proxy_url"?: string;
      "install_enabled"?: boolean;
      "install_proxy_url"?: string;
    };
    "NetworkProxyEndpoint": {
      "authentication_configured": boolean;
      "configured": boolean;
      "effective_for_next_task": true;
      "enabled": boolean;
      "requires_restart": false;
      "scheme"?: "http" | "https" | "socks5" | "socks5h";
      "source": "managed" | "environment";
      "url": string;
    };
    "NetworkProxyTestEnvelope": {
      "data": components["schemas"]["NetworkProxyTestResult"];
      "ok": true;
    };
    "NetworkProxyTestRequest": {
      "scope": "install" | "community";
    };
    "NetworkProxyTestResult": {
      "bridge_enabled"?: boolean;
      "diagnostic"?: string;
      "docker_latency_ms"?: number;
      "docker_ok"?: boolean;
      "failure_stage"?: string;
      "host_latency_ms": number;
      "host_network"?: boolean;
      "host_ok": boolean;
      "http_status": number;
      "latency_ms": number;
      "message": string;
      "ok": boolean;
      "proxy_enabled": boolean;
      "proxy_scheme": "http" | "https" | "socks5" | "socks5h";
      "scope": "install" | "community";
      "target": string;
    };
    "PalDefenderAccessSettingsUpdate": {
      "admin_auto_login": boolean;
      "admin_ips": Array<string>;
      "use_admin_whitelist": boolean;
      "use_whitelist": boolean;
      "whitelist_message": string;
    };
    "PalDefenderBroadcastRequest": {
      "alert"?: boolean;
      "message": string;
    };
    "PalDefenderExportPalsEnvelope": {
      "data": components["schemas"]["PalDefenderExportPalsResult"];
      "ok": true;
    };
    "PalDefenderExportPalsResult": {
      "command": string;
      "output": string;
      "player_id": string;
      "template": components["schemas"]["PalDefenderPalTemplate"];
      "template_info": components["schemas"]["PalDefenderExportedPalTemplateInfo"];
      "templates": Array<components["schemas"]["PalDefenderExportedPalTemplateInfo"]>;
    };
    "PalDefenderExportedPalTemplateInfo": {
      "modified_at": string;
      "name": string;
      "path": string;
      "player_id": string;
      "size": number;
    };
    "PalDefenderGMInventory": {
      "Inventory": components["schemas"]["PalDefenderInventory"];
      "Meta": {
        "Player": string;
        "PlayerUID": string;
      };
    };
    "PalDefenderGMPlayer": {
      "GuildName": string;
      "GuildUUID": string;
      "IP": string;
      "MapLocation": components["schemas"]["PalDefenderLocation"];
      "Name": string;
      "PlayerUID": string;
      "Status": string;
      "UserId": string;
      "WorldLocation": components["schemas"]["PalDefenderLocation"];
    };
    "PalDefenderGMPlayers": {
      "Meta": {
        "OnlineCount": number;
        "PlayerCount": number;
      };
      "Players": Array<components["schemas"]["PalDefenderGMPlayer"]>;
    };
    "PalDefenderGMStatus": {
      "available": boolean;
      "configured": boolean;
      "error"?: string;
      "installed": boolean;
      "load_verified": boolean;
      "rest_enabled": boolean;
      "state": "ready" | "not_installed" | "not_loaded" | "not_configured" | "rest_disabled" | "server_not_running" | "failed";
      "version"?: components["schemas"]["PalDefenderRESTVersion"];
    };
    "PalDefenderGiveCustomPalsRequest": {
      "Count": number;
      "Template": components["schemas"]["PalDefenderPalTemplate"];
    };
    "PalDefenderGiveItemsRequest": {
      "Items": Array<components["schemas"]["PalDefenderItemGrant"]>;
    };
    "PalDefenderGivePalTemplatesRequest": {
      "PalTemplates": Array<string>;
    };
    "PalDefenderGivePalsRequest": {
      "Pals": Array<components["schemas"]["PalDefenderPalGrant"]>;
    };
    "PalDefenderInventory": {
      "Armor": components["schemas"]["PalDefenderInventoryContainer"];
      "DropSlot": components["schemas"]["PalDefenderInventoryContainer"];
      "Food": components["schemas"]["PalDefenderInventoryContainer"];
      "Items": components["schemas"]["PalDefenderInventoryContainer"];
      "KeyItems": components["schemas"]["PalDefenderInventoryContainer"];
      "Weapons": components["schemas"]["PalDefenderInventoryContainer"];
    };
    "PalDefenderInventoryContainer": {
      "Available": boolean;
      "ContainerID": string;
      "FreeSlots": number;
      "MaxSlots": number;
      "Slots": Record<string, components["schemas"]["PalDefenderInventorySlot"]>;
      "UsedSlots": number;
    };
    "PalDefenderInventorySlot": {
      "Count": number;
      "ItemID": string;
    };
    "PalDefenderItemCatalog": {
      "items": Array<components["schemas"]["PalDefenderItemCatalogEntry"]>;
      "returned": number;
    };
    "PalDefenderItemCatalogEntry": {
      "collaboration"?: string;
      "icon"?: string;
      "id": string;
      "name": string;
    };
    "PalDefenderItemGrant": {
      "Count": number;
      "ItemID": string;
    };
    "PalDefenderLocation": {
      "x": number;
      "y": number;
      "z": number;
    };
    "PalDefenderMessageRequest": {
      "Message": string;
      "SendType"?: "PlayerChat" | "PlayerGlobalChat" | "PlayerGuildChat" | "PlayerLogNormal" | "PlayerLogImportant" | "PlayerLogVeryImportant";
    };
    "PalDefenderPalCatalog": {
      "items": Array<components["schemas"]["PalDefenderPalCatalogEntry"]>;
      "returned": number;
    };
    "PalDefenderPalCatalogEntry": {
      "id": string;
      "kind"?: string;
      "name": string;
    };
    "PalDefenderPalGrant": {
      "Level": number;
      "PalID": string;
    };
    "PalDefenderPalTemplate": {
      "ActiveSkills"?: Array<string>;
      "CondensedPals"?: number;
      "CraftSpeed"?: number;
      "DisableWorkPreferences"?: Array<string>;
      "Exp"?: number;
      "ExtraWorkSuitabilities"?: Record<string, number>;
      "FriendshipPoints"?: number;
      "Gender"?: "Male" | "Female" | "None";
      "HP"?: number;
      "Hunger"?: number;
      "IVs"?: Record<string, number>;
      "ImportedCharacter"?: boolean;
      "IsAwakening"?: boolean;
      "LearntSkills"?: Array<string>;
      "Level"?: number;
      "MP"?: number;
      "MaxHunger"?: number;
      "Nickname"?: string;
      "PalID": string;
      "PalSouls"?: Record<string, number>;
      "PartnerSkillLevel"?: number;
      "Passives"?: Array<string>;
      "PhysicalHealth"?: string;
      "SAN"?: number;
      "SP"?: number;
      "Shield"?: number;
      "Shiny"?: boolean;
      "SkinId"?: string;
      "Support"?: number;
      "UniqueNPCID"?: string;
      "UnusedStatusPoints"?: number;
      "WorkerSick"?: string;
    };
    "PalDefenderProgressionGrantRequest": {
      "AncientTechnologyPoints"?: number;
      "EXP"?: number;
      "Relics"?: Record<string, number>;
      "TechnologyPoints"?: number;
    };
    "PalDefenderPunishmentRequest": {
      "IP"?: boolean;
      "Reason"?: string;
    };
    "PalDefenderRESTVersion": {
      "Beta": boolean;
      "Build": number;
      "Major": number;
      "Minor": number;
      "Patch": number;
      "Version": string;
      "VersionLong": string;
    };
    "PalDefenderReleasePalRequest": {
      "Gender"?: "male" | "female";
      "Level"?: number;
      "Lucky"?: boolean;
      "PalID": string;
      "Rank"?: number;
    };
    "PalDefenderRemoveItemsRequest": {
      "Items": Array<components["schemas"]["PalDefenderItemGrant"]>;
    };
    "PalDefenderTechnologyCatalog": {
      "items": Array<components["schemas"]["PalDefenderTechnologyCatalogEntry"]>;
      "returned": number;
    };
    "PalDefenderTechnologyCatalogEntry": {
      "boss": boolean;
      "category": string;
      "icon_url"?: string;
      "id": string;
      "level": number;
      "name": string;
    };
    "PalDefenderTechnologyRequest": {
      "Technology": unknown;
    };
    "PalDefenderTeleportRequest": unknown;
    "PalworldConfig": {
      "draft"?: components["schemas"]["PalworldConfigDraft"];
      "format_issues": Array<components["schemas"]["PalworldFormatIssue"]>;
      "issues": Array<components["schemas"]["PalworldValidationIssue"]>;
      "path": string;
      "pending_restart": boolean;
      "revision_sha256": string;
      "secret_state": components["schemas"]["PalworldSecretState"];
      "settings": Record<string, string>;
    };
    "PalworldConfigApplyRequest": {
      "draft_id": string;
    };
    "PalworldConfigDraft": {
      "applied_job_id"?: string;
      "created_at": string;
      "id": string;
      "revision_sha256": string;
      "status": "draft" | "applying" | "completed" | "failed" | "stale" | "expired" | "superseded";
      "updated_at": string;
    };
    "PalworldConfigEnvelope": {
      "data": components["schemas"]["PalworldConfig"];
      "ok": true;
    };
    "PalworldConfigFieldSchema": {
      "default"?: string | null;
      "description": string;
      "enum"?: Array<string>;
      "enum_labels"?: Record<string, string>;
      "group": string;
      "key": string;
      "label": string;
      "max"?: number;
      "min"?: number;
      "requires_restart": boolean;
      "risk"?: string;
      "type": "string" | "bool" | "int" | "float" | "enum" | "list";
    };
    "PalworldConfigRevision": {
      "changed_fields": Array<string>;
      "created_at": string;
      "current": boolean;
      "id": string;
      "parent_sha256"?: string;
      "revision_sha256": string;
      "source": "baseline" | "apply";
    };
    "PalworldConfigRevisionDiff": {
      "changes": Array<components["schemas"]["PalworldConfigRevisionFieldDiff"]>;
      "current_sha256": string;
      "revision_id": string;
      "revision_sha256": string;
    };
    "PalworldConfigRevisionDiffEnvelope": {
      "data": components["schemas"]["PalworldConfigRevisionDiff"];
      "ok": true;
    };
    "PalworldConfigRevisionFieldDiff": {
      "current_configured": boolean;
      "current_value"?: string;
      "field": string;
      "revision_configured": boolean;
      "revision_value"?: string;
      "secret": boolean;
    };
    "PalworldConfigRevisionList": {
      "current_revision_sha256": string;
      "items": Array<components["schemas"]["PalworldConfigRevision"]>;
      "retention": number;
    };
    "PalworldConfigRevisionListEnvelope": {
      "data": components["schemas"]["PalworldConfigRevisionList"];
      "ok": true;
    };
    "PalworldConfigRevisionRestoreRequest": {
      "confirm": true;
    };
    "PalworldConfigSchema": {
      "fields": Array<components["schemas"]["PalworldConfigFieldSchema"]>;
      "version": string;
    };
    "PalworldConfigSchemaEnvelope": {
      "data": components["schemas"]["PalworldConfigSchema"];
      "ok": true;
    };
    "PalworldConfigUpdateRequest": {
      "clear_secrets"?: Array<"AdminPassword" | "ServerPassword">;
      "settings": Record<string, unknown>;
    };
    "PalworldConfigValidateRequest": {
      "clear_secrets"?: Array<"AdminPassword" | "ServerPassword">;
      "settings": Record<string, unknown>;
    };
    "PalworldConfigValidation": {
      "issues": Array<components["schemas"]["PalworldValidationIssue"]>;
      "valid": boolean;
    };
    "PalworldConfigValidationEnvelope": {
      "data": components["schemas"]["PalworldConfigValidation"];
      "ok": true;
    };
    "PalworldFormatIssue": {
      "code": string;
      "field": string;
      "message": string;
      "severity": "warning" | "error";
    };
    "PalworldSecretConfigured": {
      "configured": boolean;
    };
    "PalworldSecretState": {
      "admin_password": components["schemas"]["PalworldSecretConfigured"];
      "server_password": components["schemas"]["PalworldSecretConfigured"];
    };
    "PalworldValidationIssue": {
      "field"?: string;
      "message": string;
      "severity": "warning" | "error";
    };
    "PasswordChangeRequest": {
      "current_password": string;
      "new_password": string;
    };
    "PatchInfo": {
      "build": {
        "build_time": string;
        "commit": string;
        "version": string;
      };
      "compatibility": {
        "target_version": "v1.3.0";
        "verified": true;
      };
      "patch": {
        "features": Array<string>;
        "repository": "ninhua/palworld-panel";
        "version": "0.8.83";
      };
      "upstream": {
        "commit": string;
        "ref": "v1.3.0";
        "repository": "uitok/palworld-panel";
      };
    };
    "PatchInfoEnvelope": {
      "data": components["schemas"]["PatchInfo"];
      "ok": true;
    };
    "Player": {
      "annotation_updated_at"?: string;
      "gm_user_id"?: string;
      "guild_id": string;
      "guild_name": string;
      "has_annotation"?: boolean;
      "id": string;
      "inventory_summary"?: Record<string, unknown>;
      "ip"?: string;
      "is_online": boolean;
      "last_offline_at"?: string;
      "last_online_at"?: string;
      "last_online_time": string;
      "last_seen_at"?: string;
      "level": number;
      "location_x": number;
      "location_y": number;
      "location_z": number;
      "nickname": string;
      "note"?: string;
      "online_source": "none" | "rest" | "paldefender" | "rest+paldefender";
      "online_stale": boolean;
      "ping"?: number;
      "player_uid": string;
      "presence_available"?: boolean;
      "presence_observed_at"?: string;
      "presence_online"?: boolean;
      "presence_sessions"?: Array<components["schemas"]["PlayerPresenceSession"]>;
      "presence_stale"?: boolean;
      "session_seconds"?: number;
      "session_started_at"?: string;
      "steam_id": string;
      "tags"?: Array<string>;
      "total_seconds"?: number;
    };
    "PlayerAnnotationUpdate": {
      "note"?: string;
      "tags"?: Array<string>;
    };
    "PlayerDataView": {
      "online_overlay": boolean;
      "scope": "active" | "server";
      "source_id": string;
      "source_kind": "server" | "import";
      "source_name": string;
    };
    "PlayerDetailEnvelope": {
      "data": components["schemas"]["PlayerDetailResult"];
      "ok": true;
    };
    "PlayerDetailResult": {
      "player": components["schemas"]["Player"];
      "status": components["schemas"]["SaveIndexStatus"];
      "view": components["schemas"]["PlayerDataView"];
    };
    "PlayerInventoryEnvelope": {
      "data": components["schemas"]["PlayerInventoryResult"];
      "ok": true;
    };
    "PlayerInventoryResult": {
      "containers": Array<components["schemas"]["SaveInventoryContainer"]>;
      "status": components["schemas"]["SaveIndexStatus"];
      "view": components["schemas"]["PlayerDataView"];
    };
    "PlayerListEnvelope": {
      "data": components["schemas"]["PlayerListResult"];
      "ok": true;
    };
    "PlayerListResult": {
      "players": Array<components["schemas"]["Player"]>;
      "status": components["schemas"]["SaveIndexStatus"];
      "summary": components["schemas"]["ListSummary"];
      "view": components["schemas"]["PlayerDataView"];
    };
    "PlayerPresenceSession": {
      "duration_seconds": number;
      "ended_at": string;
      "started_at": string;
    };
    "SafeLifecycleRequest": {
      "message"?: string;
      "waittime"?: number;
    };
    "SaveHistoryChange": {
      "category": "players" | "guilds" | "bases" | "pals" | "containers" | "items";
      "delta"?: number;
      "fields": Array<components["schemas"]["SaveHistoryFieldChange"]>;
      "id": string;
      "kind": "added" | "removed" | "changed" | "increased" | "decreased";
      "label": string;
    };
    "SaveHistoryDiff": {
      "event_total": number;
      "events": Array<components["schemas"]["SaveHistoryEvent"]>;
      "from": components["schemas"]["SaveHistorySnapshot"];
      "items": Array<components["schemas"]["SaveHistoryChange"]>;
      "limit": number;
      "offset": number;
      "summary": components["schemas"]["SaveHistoryDiffSummary"];
      "to": components["schemas"]["SaveHistorySnapshot"];
      "total": number;
    };
    "SaveHistoryDiffEnvelope": {
      "data": components["schemas"]["SaveHistoryDiff"];
      "ok": true;
    };
    "SaveHistoryDiffSummary": {
      "bases_added": number;
      "bases_changed": number;
      "bases_removed": number;
      "containers_added": number;
      "containers_changed": number;
      "containers_removed": number;
      "guilds_added": number;
      "guilds_changed": number;
      "guilds_removed": number;
      "items_decreased": number;
      "items_increased": number;
      "pals_added": number;
      "pals_changed": number;
      "pals_removed": number;
      "players_added": number;
      "players_changed": number;
      "players_removed": number;
    };
    "SaveHistoryEvent": {
      "actor_id": string;
      "actor_label": string;
      "actor_type": string;
      "after": string;
      "before": string;
      "category": "players" | "guilds" | "bases" | "pals" | "containers" | "items";
      "delta"?: number;
      "details": Array<components["schemas"]["SaveHistoryFieldChange"]>;
      "id": string;
      "inferred": true;
      "kind": string;
      "metadata": Record<string, string>;
      "subject_id": string;
      "subject_label": string;
      "subject_type": string;
      "target_id": string;
      "target_label": string;
      "target_type": string;
    };
    "SaveHistoryFieldChange": {
      "after": string;
      "before": string;
      "field": string;
    };
    "SaveHistorySnapshot": {
      "captured_at": string;
      "counts": components["schemas"]["SaveIndexCounts"];
      "fingerprint": string;
      "generated_at": string;
      "id": string;
      "parser": string;
      "size_bytes": number;
    };
    "SaveHistorySource": {
      "id": string;
      "kind": "server" | "import";
      "name": string;
    };
    "SaveHistoryState": {
      "items": Array<components["schemas"]["SaveHistorySnapshot"]>;
      "max_total_bytes": number;
      "minimum_interval_seconds": number;
      "retention": number;
      "source": components["schemas"]["SaveHistorySource"];
      "total_bytes": number;
    };
    "SaveHistoryStateEnvelope": {
      "data": components["schemas"]["SaveHistoryState"];
      "ok": true;
    };
    "SaveImportCandidate": {
      "errors": Array<string>;
      "id": string;
      "level_sha256": string;
      "level_size": number;
      "player_count": number;
      "relative_path": string;
      "valid": boolean;
      "warnings": Array<string>;
      "world_id"?: string;
    };
    "SaveImportCommitRequest": {
      "candidate_id"?: string;
      "confirm"?: true;
      "inspection_id"?: string;
      "migration_source_id"?: string;
      "name"?: string;
      "steam_id"?: string;
    };
    "SaveImportConflictEnvelope": {
      "error": {
        "candidates"?: Array<components["schemas"]["SaveImportCandidate"]>;
        "code": string;
        "expires_at"?: string;
        "inspection_id"?: string;
        "message": string;
      };
      "ok": false;
    };
    "SaveImportInspectRequest": {
      "file": string;
      "name"?: string;
    };
    "SaveImportInspection": {
      "candidates": Array<components["schemas"]["SaveImportCandidate"]>;
      "expires_at": string;
      "file_name": string;
      "id": string;
      "name"?: string;
      "requires_selection": boolean;
      "selected_candidate_id": string;
    };
    "SaveImportInspectionEnvelope": {
      "data": components["schemas"]["SaveImportInspection"];
      "ok": true;
    };
    "SaveImportSelectRequest": {
      "candidate_id": string;
    };
    "SaveIndexCounts": {
      "bases": number;
      "containers": number;
      "guilds": number;
      "map_entities": number;
      "pals": number;
      "players": number;
    };
    "SaveIndexStatus": {
      "cache_path"?: string;
      "counts": components["schemas"]["SaveIndexCounts"];
      "duration_ms": number;
      "enabled": boolean;
      "error"?: string;
      "error_code"?: string;
      "error_detail"?: string;
      "oodle_available"?: boolean;
      "parser"?: string;
      "source_path": string;
      "stale": boolean;
      "state": "disabled" | "missing" | "not_indexed" | "ready" | "stale" | "error";
      "updated_at": string;
      "warnings": Array<string>;
    };
    "SaveIndexStatusEnvelope": {
      "data": components["schemas"]["SaveIndexStatus"];
      "ok": true;
    };
    "SaveInventoryContainer": {
      "container_id": string;
      "container_name": string;
      "container_type": string;
      "owner_id": string;
      "owner_type": string;
      "slots": Array<components["schemas"]["SaveInventorySlot"]>;
    };
    "SaveInventorySlot": {
      "count": number;
      "durability": number | null;
      "item_icon": string;
      "item_id": string;
      "item_name": string;
      "slot": number;
    };
    "SaveSource": {
      "active": boolean;
      "created_at": string;
      "fingerprint"?: string;
      "id": string;
      "indexed_at"?: string;
      "kind": "server" | "import";
      "name": string;
      "parser_version"?: string;
      "path"?: string;
      "updated_at": string;
      "warnings"?: Array<string>;
    };
    "SaveSourceImportRequest": {
      "file": string;
      "name"?: string;
    };
    "Schedule": components["schemas"]["ScheduleInput"] & {
      "created_at": string;
      "enabled": boolean;
      "id": string;
      "last_run_at"?: string;
      "next_run_at"?: string;
      "timezone": string;
      "updated_at": string;
    };
    "ScheduleInput": {
      "enabled"?: boolean;
      "interval_minutes"?: number;
      "message"?: string;
      "time_of_day"?: string;
      "timezone"?: string;
      "type": "save" | "backup" | "safe_restart" | "update" | "version_check";
      "waittime"?: number;
    };
    "SessionEnvelope": {
      "data": components["schemas"]["SessionInfo"];
      "ok": true;
    };
    "SessionInfo": {
      "name": string;
      "permissions": Array<string>;
      "role": "admin" | "operator" | "viewer";
    };
    "StarterGiftConfig": {
      "ancient_technology_points": number;
      "batch_delay_ms": number;
      "enabled": boolean;
      "item_batch_size": number;
      "items": Array<components["schemas"]["StarterGiftItem"]>;
      "pal_templates": Array<string>;
      "technology_mode": "none" | "unlock_all" | "grant_points";
      "technology_points": number;
      "template_batch_size": number;
    };
    "StarterGiftGrant": {
      "ancient_technology_points": number;
      "attempts": number;
      "completed_at"?: string;
      "first_seen_at": string;
      "item_total": number;
      "last_error"?: string;
      "next_item": number;
      "next_template": number;
      "nickname"?: string;
      "player_id": string;
      "player_uid"?: string;
      "progress_percent": number;
      "status": "pending" | "running" | "success" | "failed";
      "steam_id"?: string;
      "technology_done": boolean;
      "technology_mode": "none" | "unlock_all" | "grant_points";
      "technology_points": number;
      "template_total": number;
      "updated_at": string;
    };
    "StarterGiftItem": {
      "count": number;
      "item_id": string;
    };
    "StarterGiftSnapshot": {
      "config": components["schemas"]["StarterGiftConfig"];
      "grants": Array<components["schemas"]["StarterGiftGrant"]>;
      "item_catalog": Array<components["schemas"]["PalDefenderItemCatalogEntry"]>;
      "template_error"?: string;
      "template_indexes"?: Array<components["schemas"]["StarterGiftTemplateIndexInfo"]>;
      "templates": Array<components["schemas"]["StarterGiftTemplateInfo"]>;
    };
    "StarterGiftTemplateIndexInfo": {
      "count": number;
      "label": string;
      "name": string;
    };
    "StarterGiftTemplateInfo": {
      "category"?: string;
      "english_name"?: string;
      "index_names"?: Array<string>;
      "level"?: number;
      "modified_at"?: string;
      "name": string;
      "nickname"?: string;
      "pal_id"?: string;
      "pal_name"?: string;
      "parse_error"?: string;
      "size"?: number;
    };
    "SteamWorkshopAuthRequest": {
      "account_name"?: string;
      "password"?: string;
      "steam_guard_code"?: string;
    };
    "SteamWorkshopAuthStatus": {
      "account_name"?: string;
      "credentials_secure": boolean;
      "last_verified_at"?: string;
      "logged_in": boolean;
      "login_in_progress": boolean;
      "message"?: string;
      "password_configured": boolean;
      "steam_guard_required": boolean;
      "steamcmd_installed": boolean;
      "supported": boolean;
      "verification_required": boolean;
    };
    "SteamWorkshopAuthStatusEnvelope": {
      "data": components["schemas"]["SteamWorkshopAuthStatus"];
      "ok": true;
    };
    "SuccessEnvelope": {
      "data": unknown;
      "ok": true;
    };
    "SupportBundleCheck": {
      "id": string;
      "message": string;
      "ok": boolean;
      "required": boolean;
    };
    "SupportBundleCreateRequest": {
      "confirm": true;
      "include_logs"?: boolean;
    };
    "SupportBundleEnvelope": {
      "data": components["schemas"]["SupportBundleMetadata"];
      "ok": true;
    };
    "SupportBundleListEnvelope": {
      "data": Array<components["schemas"]["SupportBundleMetadata"]>;
      "ok": true;
    };
    "SupportBundleMetadata": {
      "created_at": string;
      "entries": Array<string>;
      "file_name": string;
      "id": string;
      "include_logs": boolean;
      "sha256": string;
      "size_bytes": number;
    };
    "SupportBundleStatus": {
      "checks": Array<components["schemas"]["SupportBundleCheck"]>;
      "directory_ready": boolean;
      "max_bundle_bytes": number;
      "max_bundles": number;
      "max_log_bytes_per_file": number;
      "max_log_files": number;
      "retention_days": number;
      "schema_version": 1;
    };
    "SupportBundleStatusEnvelope": {
      "data": components["schemas"]["SupportBundleStatus"];
      "ok": true;
    };
    "WebDAVConfig": {
      "base_url": string;
      "enabled": boolean;
      "password_configured": boolean;
      "remote_path": string;
      "upload_after_backup": boolean;
      "username": string;
    };
    "WebDAVConfigUpdate": {
      "base_url"?: string;
      "clear_password"?: boolean;
      "enabled"?: boolean;
      "password"?: string;
      "remote_path"?: string;
      "upload_after_backup"?: boolean;
      "username"?: string;
    };
  };
}
