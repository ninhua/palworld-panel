import type { components } from '../api/generated/contracts';

export type RuntimeMode = 'wine_docker' | 'windows_steamcmd';
export type Role = 'admin' | 'operator' | 'viewer';
export type Permission =
  | 'read'
  | 'server:control'
  | 'config:write'
  | 'backup:write'
  | 'mods:write'
  | 'players:write'
  | 'security:write'
  | 'audit:read'
  | 'world:reset'
  | 'ai:config';

export interface SessionInfo {
  name: string;
  role: Role;
  permissions: Permission[];
}

export interface AuthStatus {
	initialized: boolean;
	authenticated: boolean;
	user?: SessionInfo;
}

export interface DevelopmentKey {
	id: string;
	name: string;
	prefix: string;
	created_at: string;
	last_used_at?: string;
	revoked_at?: string;
	token?: string;
}

export type ServerProcessStatus = 'running' | 'stopped' | 'starting' | 'stopping' | 'updating' | 'error';

export interface ContainerStatus {
  exists: boolean;
  status: string;
}

export interface ServerStatus {
  status: ServerProcessStatus;
  installed: boolean;
  pending_restart: boolean;
  runtime_mode: RuntimeMode;
  setup_step: 'prerequisites' | 'installed' | 'config_initialized' | 'configured' | 'started' | 'ready' | string;
  config_exists: boolean;
  container: ContainerStatus;
  startup_args: string[];
  ports: Record<string, number>;
  warnings: string[];
  paths: Record<string, string>;
  server_imported: boolean;
  cpu_percent?: number;
  memory_usage_bytes?: number;
  port?: number;
  settings_path?: string;
  pid?: number;
  version?: string;
}

export interface CrashGuardEvent {
  id: string;
  kind: 'unexpected_exit' | 'container_restart' | 'oom_kill';
  runtime_mode: string;
  occurrences: number;
  exit_code: number;
  oom_killed: boolean;
  restart_count: number;
  started_at?: string;
  finished_at?: string;
  message: string;
  created_at: string;
}

export interface CrashGuardStatus {
  enabled: boolean;
  tripped: boolean;
  tripped_at?: string;
  reason?: string;
  expected_operation: boolean;
  recent_crash_count: number;
  threshold: number;
  window_seconds: number;
  last_observed_status?: string;
  last_observed_runtime?: string;
  updated_at: string;
  events: CrashGuardEvent[];
}

export interface ServerVersionInfo {
  installed: boolean;
  current_build_id: string;
  latest_build_id: string;
  update_available: boolean;
  last_checked_at: string;
  source: string;
  manifest_path: string;
  game_version?: string;
  compatibility_target?: string;
  compatible?: boolean;
  compatibility_warnings: string[];
  error?: string;
}

export interface ServerMetrics {
  server_fps?: number;
  current_players?: number;
  max_players?: number;
  uptime?: number;
  total_pals?: number;
  active_bases?: number;
  frame_time?: number;
  days?: number;
}

export interface ServerLogResponse {
  logs: string;
  source: 'file' | 'docker' | 'paldefender-game' | 'paldefender-rest' | 'none';
  available: boolean;
  reason?: string;
  updated_at?: string;
}

export interface WorldInfo {
  active_world_id: string;
  save_exists: boolean;
  last_modified?: string;
  server_running: boolean;
  reset_available: boolean;
  reset_unavailable_reason?: string;
}

export interface MonitorSample {
  id: string;
  created_at: string;
  cpu_available: boolean;
  cpu_percent: number;
  memory_available: boolean;
  memory_usage_bytes: number;
  memory_limit_bytes: number;
  host_memory_available: boolean;
  host_memory_total_bytes: number;
  host_memory_available_bytes: number;
  host_swap_total_bytes: number;
  host_swap_free_bytes: number;
  workload_memory_available: boolean;
  workload_memory_usage_bytes: number;
  workload_memory_limit_bytes: number;
  oom_killed: boolean;
  lifecycle_available: boolean;
  exit_code: number;
  restart_count: number;
  started_at?: string;
  finished_at?: string;
  risk_reasons: MonitorRiskReason[];
  disk_available: boolean;
  disk_free_bytes: number;
  disk_total_bytes: number;
  current_players: number;
  max_players: number;
  rest_healthy: boolean;
  rcon_healthy: boolean;
  game_port_healthy: boolean;
  query_port_healthy: boolean;
  unavailable_reason?: string;
}

export interface MonitorSnapshot {
  sample: MonitorSample;
}

export interface DebugLogStatus {
  enabled: boolean;
  path: string;
  size: number;
  max_bytes: number;
  max_files: number;
}

export interface Prerequisite {
  id: string;
  label: string;
  ok: boolean;
  required: boolean;
  message?: string;
}

export interface DockerCapability {
  cli_installed: boolean;
  cli_path?: string;
  daemon_reachable: boolean;
  version?: string;
  error?: string;
}

export interface SudoCapability {
  is_root: boolean;
  sudo_installed: boolean;
  passwordless: boolean;
  can_elevate: boolean;
  needs_password: boolean;
  message?: string;
}

export interface HostCapabilities {
  os: string;
  arch: string;
  distro_id?: string;
  distro_name?: string;
  distro_version?: string;
  distro_codename?: string;
  package_manager?: string;
  systemd: boolean;
  supported: boolean;
  unsupported_reason?: string;
  recommended_runtime: RuntimeMode;
  docker: DockerCapability;
  sudo: SudoCapability;
  current_user?: string;
  current_user_in_docker_group: boolean;
  warnings?: string[];
}

export type DockerSourceID = 'auto' | 'official' | 'aliyun' | 'azurecn';
export type DockerMirrorID =
  | 'auto'
  | 'daocloud'
  | 'one_ms'
  | 'registry_cyou'
  | 'dockerproxy_net'
  | 'dockerproxy_link'
  | 'docker_jiaxin'
  | 'docker_xuanyuan'
  | 'free_hubfast';

export interface DockerInstallSource {
  id: string;
  name: string;
  url: string;
  probe_url: string;
  available: boolean;
  latency_ms?: number;
  error?: string;
  selected: boolean;
}

export interface DockerInstallPlan {
  host: HostCapabilities;
  source: string;
  source_url?: string;
  sources: DockerInstallSource[];
  supported: boolean;
  can_auto_install: boolean;
  requires_manual: boolean;
  docker_installed: boolean;
  docker_ready: boolean;
  error_code?: string;
  message?: string;
  manual_command?: string;
  script?: string;
  script_path?: string;
  warnings?: string[];
}

export interface DockerInstallRequest {
  source?: DockerSourceID;
  add_current_user_to_docker_group?: boolean;
}

export interface DockerRegistryMirror {
  id: string;
  name: string;
  url: string;
  probe_url: string;
  available: boolean;
  latency_ms?: number;
  error?: string;
  selected: boolean;
}

export interface DockerMirrorPlan {
  host: HostCapabilities;
  mirror: string;
  mirrors: DockerRegistryMirror[];
  selected_mirrors: string[];
  existing_mirrors?: string[];
  config_path: string;
  supported: boolean;
  can_auto_configure: boolean;
  requires_manual: boolean;
  docker_installed: boolean;
  docker_ready: boolean;
  error_code?: string;
  message?: string;
  manual_command?: string;
  script?: string;
  script_path?: string;
  warnings?: string[];
}

export interface DockerMirrorRequest {
  mirror?: DockerMirrorID;
}

export interface ValidationIssue {
  field?: string;
  severity: 'error' | 'warning' | 'info' | string;
  message: string;
}

export interface StartupConfig {
  port: number;
  players: number;
  public_lobby: boolean;
  public_ip?: string;
  public_port?: number;
  log_format: 'text' | 'json' | 'Text' | 'Json' | string;
  use_perf_threads: boolean;
  no_async_loading_thread: boolean;
  use_multithread_for_ds: boolean;
  number_of_worker_threads_server?: number;
  workshop_dir?: string;
  no_mods: boolean;
}

export interface StartupResponse {
  startup: StartupConfig;
  args: string[];
  issues: ValidationIssue[];
}

export type FieldType = 'string' | 'bool' | 'int' | 'float' | 'enum' | 'list';

export interface FieldSchema {
  key: string;
  label?: string;
  group: string;
  type: FieldType;
  default?: string;
  enum?: string[];
  enum_labels?: Record<string, string>;
  min?: number;
  max?: number;
  requires_restart: boolean;
  risk?: string;
  description: string;
}

export type PalworldSettings = Record<string, string | number | boolean | undefined>;

export interface PalworldConfigDraft {
  id: string;
  revision_sha256: string;
  status: 'draft' | 'applying' | 'completed' | 'failed' | string;
  created_at: string;
  updated_at: string;
  applied_job_id?: string;
}

export interface MonitorRiskReason {
  code: string;
  message: string;
  severity: 'warning' | 'critical';
}

export interface PalworldSecretState {
  admin_password: { configured: boolean };
  server_password: { configured: boolean };
}

export interface PalworldFormatIssue extends ValidationIssue {
  code: string;
}

export interface PalworldConfigResponse {
  settings: PalworldSettings;
  path: string;
  pending_restart: boolean;
  revision_sha256?: string;
  secret_state?: PalworldSecretState;
  format_issues?: PalworldFormatIssue[];
  draft?: PalworldConfigDraft;
  issues?: ValidationIssue[];
}

export interface PalworldConfigRevision {
  id: string;
  revision_sha256: string;
  parent_sha256?: string;
  source: 'baseline' | 'apply' | string;
  changed_fields: string[];
  created_at: string;
  current: boolean;
}

export interface PalworldConfigRevisionList {
  current_revision_sha256: string;
  retention: number;
  items: PalworldConfigRevision[];
}

export interface PalworldConfigRevisionFieldDiff {
  field: string;
  secret: boolean;
  revision_value?: string;
  current_value?: string;
  revision_configured: boolean;
  current_configured: boolean;
}

export interface PalworldConfigRevisionDiff {
  revision_id: string;
  revision_sha256: string;
  current_sha256: string;
  changes: PalworldConfigRevisionFieldDiff[];
}

export interface PalworldSchemaResponse {
  version: string;
  fields: FieldSchema[];
}

export interface PalworldValidateResponse {
  valid: boolean;
  issues: ValidationIssue[];
}

type JobContract = components['schemas']['Job'];

export interface Job extends Omit<JobContract, 'type' | 'status' | 'message' | 'updated_at'> {
  id: string;
  type:
    | 'backup'
    | 'restart'
    | 'broadcast'
    | 'install'
    | 'update'
    | 'save'
    | 'shutdown'
    | 'bootstrap'
    | 'workshop_download'
    | 'paldefender_install'
    | 'paldefender_update'
    | 'safe_restart'
    | 'safe_stop'
    | 'restore'
    | 'version_check'
    | 'smart_update'
    | string;
  status: 'waiting' | 'running' | 'success' | 'failed';
  progress: number;
  message?: string;
  error?: string;
  error_code?: string;
  created_at: string;
  updated_at?: string;
  finished_at?: string;
}

export interface ModItem {
  id: string;
  name: string;
  source: 'upload' | 'workshop' | string;
  package_name: string;
  path: string;
  version?: string;
  enabled: boolean;
  workshop_id?: string;
  preview_url?: string;
  steam_url?: string;
  summary?: string;
  tags?: string[];
  file_size?: number;
  subscriptions?: number;
  time_updated?: number;
  last_checked_at?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ModConfigFile {
  id: string;
  name: string;
  path: string;
  extension: string;
  size: number;
  modified_at: string;
  revision: string;
  executable: boolean;
  risk?: string;
}

export interface ModConfigurationField {
  path: string;
  label: string;
  type: 'boolean' | 'integer' | 'number' | 'string';
  value: unknown;
  min?: number;
  max?: number;
  description?: string;
  group?: string;
  options?: Array<{ value: unknown; label: string }>;
  unit?: string;
}

export interface ModConfigDocument {
  file: ModConfigFile;
  content: string;
  format: string;
  fields?: ModConfigurationField[];
}

export interface ModConfigurationAdapter {
  id: string;
  name: string;
  description: string;
  workshop_id?: string;
  available: boolean;
  installed: boolean;
  configured: boolean;
  enabled: boolean;
  status: 'not_installed' | 'dependency_missing' | 'not_configured' | 'disabled' | 'restart_required' | 'ready';
  status_detail?: string;
  reload_behavior: 'online_reload' | 'restart_required' | string;
  dependencies: Array<{ id: string; name: string; workshop_id?: string; mod_id?: string; installed: boolean; enabled: boolean; required: boolean }>;
  actions: Array<{ id: 'initialize' | 'enable'; available: boolean; restart_required: boolean }>;
  reference_urls?: Record<string, string>;
  files: ModConfigFile[];
}

export interface ModConfigurationActionResult {
  adapter: ModConfigurationAdapter;
  document?: ModConfigDocument;
  changed: string[];
  restart_required: boolean;
}

export interface ModConfigBackup {
  id: string;
  revision: string;
  size: number;
  created_at: string;
}

export type ImportCandidate = components['schemas']['ImportCandidate'];
export type ImportInspection = components['schemas']['ImportInspection'];
export type ModImportRequest = components['schemas']['ModImportRequest'];
export type LocalModFinding = components['schemas']['LocalModFinding'];
export type LocalScanResult = components['schemas']['LocalScanResult'];
export type LocalModAction = components['schemas']['LocalModActionCapability']['action'];
export type LocalModActionRequest = components['schemas']['LocalModActionRequest'];
export type LocalModActionResult = components['schemas']['LocalModActionResult'];

export interface WorkshopItem {
  id: string;
  title: string;
  summary?: string;
  preview_url?: string;
  steam_url: string;
  tags: string[];
  file_size?: number;
  subscriptions?: number;
  time_created?: number;
  time_updated?: number;
  installed: boolean;
  enabled: boolean;
  update_available: boolean;
  mod_id?: string;
  translation?: AITranslation;
}

export interface AITranslation {
  text: string;
  target_language: string;
  model: string;
  generated_at: string;
  cached: boolean;
}

export type AITranslationConfig = components['schemas']['AITranslationConfig'];
export type AITranslationConfigUpdate = components['schemas']['AITranslationConfigUpdate'];
export type AITranslationTestResult = components['schemas']['AITranslationTestResult'];
export type NetworkProxyConfig = components['schemas']['NetworkProxyConfig'];
export type NetworkProxyConfigUpdate = components['schemas']['NetworkProxyConfigUpdate'];
export type NetworkProxyEndpoint = components['schemas']['NetworkProxyEndpoint'];
export type NetworkProxyTestRequest = components['schemas']['NetworkProxyTestRequest'];
export type NetworkProxyTestResult = components['schemas']['NetworkProxyTestResult'];

export interface WorkshopSearchResponse {
  items: WorkshopItem[];
  next_cursor?: string;
  total: number;
  page_size: number;
}

export interface WorkshopStatus {
  configured: boolean;
  key_source?: 'environment' | 'bundled' | '';
  app_id: string;
}

export interface ServerImportResult {
  path: string;
  manifest_path: string;
  build_id: string;
  config_exists: boolean;
  already_bound: boolean;
  original_input: string;
}

export type SteamWorkshopAuthStatus = components['schemas']['SteamWorkshopAuthStatus'];

export interface BackupInfo {
  name: string;
  path: string;
  size_bytes: number;
  created_at: string;
  reason?: string;
  status?: string;
}

export interface WebDAVConfig {
  enabled: boolean;
  base_url: string;
  username: string;
  remote_path: string;
  upload_after_backup: boolean;
  password_configured: boolean;
}

export interface WebDAVConfigUpdate {
  enabled?: boolean;
  base_url?: string;
  username?: string;
  password?: string;
  clear_password?: boolean;
  remote_path?: string;
  upload_after_backup?: boolean;
}

export interface BackupVerifyResult {
  name: string;
  valid: boolean;
  format: string;
  checked_files: number;
  errors: string[];
}

export interface BackupRestoreRequest {
  name: string;
}

export interface SafeRestartRequest {
  waittime: number;
  message: string;
}

export interface AuditLog {
  id: string;
  actor: string;
  role: Role | string;
  action: string;
  target?: string;
  status: 'success' | 'failed' | string;
  message?: string;
  ip?: string;
  created_at: string;
}

export interface Alert {
  id: string;
  severity: 'info' | 'warning' | 'error' | string;
  title: string;
  message: string;
  source: string;
  status: 'open' | 'acked' | string;
  created_at: string;
  ack_at?: string;
}

export type ScheduleType = 'save' | 'backup' | 'safe_restart' | 'update' | 'version_check';

type ScheduleContract = components['schemas']['Schedule'];

export interface Schedule extends Omit<ScheduleContract, 'type'> {
  id: string;
  type: ScheduleType | string;
  enabled: boolean;
  interval_minutes?: number;
  time_of_day?: string;
  timezone: string;
  waittime?: number;
  message?: string;
  last_run_at?: string;
  next_run_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ApiErrorShape {
  code?: string;
  message: string;
  status?: number;
}

export interface UnsupportedCapability {
  ok: false;
  unsupported: true;
  message: string;
}

export interface PlayerAccessEntry {
  steam_id: string;
  nickname?: string;
  reason?: string;
  created_at?: string;
  updated_at?: string;
}

export interface PalDefenderAsset {
  name: string;
  size: number;
  digest?: string;
  browser_download_url: string;
}

export interface PalDefenderRelease {
  tag_name: string;
  name: string;
  published_at: string;
  assets: PalDefenderAsset[];
}

export type UE4SSDependencyState =
  | 'not_checked'
  | 'checking'
  | 'missing'
  | 'installing'
  | 'installed'
  | 'incompatible'
  | 'failed'
  | 'rollback_required';

export interface UE4SSDependencyStatus {
  state: UE4SSDependencyState;
  installed: boolean;
  version?: string;
  compatible: boolean;
  files: Record<string, boolean>;
  path: string;
  message: string;
  error?: string;
  archive_sha256?: string;
  load_verified: boolean;
  load_evidence?: string;
}

export interface PalDefenderStatus {
  installed: boolean;
  version?: string;
  release_source: string;
  needs_first_start: boolean;
  files: Record<string, boolean>;
  paths: Record<string, string>;
  rest_api_enabled: boolean;
  warnings: string[];
  ue4ss: UE4SSDependencyStatus;
  load_verified: boolean;
  load_evidence?: string;
}

export type PalDefenderGMStatus = components['schemas']['PalDefenderGMStatus'];
export type PalDefenderGMPlayer = components['schemas']['PalDefenderGMPlayer'];
export type PalDefenderGMPlayers = components['schemas']['PalDefenderGMPlayers'];
export type PalDefenderGMInventory = components['schemas']['PalDefenderGMInventory'];
export type PalDefenderInventory = components['schemas']['PalDefenderInventory'];
export type PalDefenderInventoryContainer = components['schemas']['PalDefenderInventoryContainer'];
export type PalDefenderInventorySlot = components['schemas']['PalDefenderInventorySlot'];
export type PalDefenderItemGrant = components['schemas']['PalDefenderItemGrant'];
export type PalDefenderItemCatalogEntry = components['schemas']['PalDefenderItemCatalogEntry'];
export type PalDefenderItemCatalog = components['schemas']['PalDefenderItemCatalog'];
export type PalDefenderRemoveItemsRequest = components['schemas']['PalDefenderRemoveItemsRequest'];
export type PalDefenderReleasePalRequest = components['schemas']['PalDefenderReleasePalRequest'];
export type PalDefenderPalCatalogEntry = components['schemas']['PalDefenderPalCatalogEntry'];
export type PalDefenderPalCatalog = components['schemas']['PalDefenderPalCatalog'];
export type PalDefenderTechnologyCatalogEntry = components['schemas']['PalDefenderTechnologyCatalogEntry'];
export type PalDefenderTechnologyCatalog = components['schemas']['PalDefenderTechnologyCatalog'];
export type PalDefenderMessageRequest = components['schemas']['PalDefenderMessageRequest'];
export type PalDefenderPunishmentRequest = components['schemas']['PalDefenderPunishmentRequest'];
export type PalDefenderProgressionGrantRequest = components['schemas']['PalDefenderProgressionGrantRequest'];
export type PalDefenderPalGrant = components['schemas']['PalDefenderPalGrant'];
export type PalDefenderGivePalsRequest = components['schemas']['PalDefenderGivePalsRequest'];
export type PalDefenderGivePalTemplatesRequest = components['schemas']['PalDefenderGivePalTemplatesRequest'];
export type PalDefenderGiveCustomPalsRequest = components['schemas']['PalDefenderGiveCustomPalsRequest'];
export type PalDefenderPalTemplate = components['schemas']['PalDefenderPalTemplate'];
export type PalDefenderExportedPalTemplateInfo = components['schemas']['PalDefenderExportedPalTemplateInfo'];
export type PalDefenderExportPalsResult = components['schemas']['PalDefenderExportPalsResult'];
export type PalDefenderAccessSettingsUpdate = components['schemas']['PalDefenderAccessSettingsUpdate'];

export type PalDefenderTeleportRequest =
  | { Mode: 'coordinates'; X: number; Y: number; Z?: number }
  | { Mode: 'player'; TargetPlayer: string };

export interface PalDefenderTechnologyRequest {
  Technology: string | string[];
}

export interface PalDefenderProgression {
  Meta: { PlayerUID: string; Player: string };
  Progression: {
    Player: { level: number; exp: number; unusedStatusPoints: number };
    Currencies: { relics: Record<string, number>; technologyPoints: number; ancientTechnologyPoints: number };
    Bosses: Record<string, unknown>;
    Captures: Record<string, unknown>;
    Activities: Record<string, unknown>;
  };
}

export interface PalDefenderProgressionGrantResult {
  Granted: Record<string, unknown>;
  Totals: Record<string, unknown>;
}

export interface PalDefenderTechs {
  Meta: { PlayerUID: string; Player: string; UnlockedCount: number; LockedCount: number; TotalCount: number };
  Techs: { Unlocked: string[] };
}

export interface PalDefenderTechnologyResult {
  UnlockedCount?: number;
  Unlocked?: string[];
  ForgottenCount?: number;
  Forgotten?: string[] | 'All';
  Skipped: string[];
}

export interface PalDefenderGMPals {
  Meta: { PlayerUID: string; Player: string; TeamCount: number; PalboxCount: number; BaseCampCount: number };
  Pals: Record<string, unknown>;
}

export interface PalDefenderGivePalsResult {
  Granted: { Pals: number };
}

export interface PalDefenderGivePalTemplatesResult {
  Granted: { PalTemplates: number };
}

export interface PalDefenderPalTemplateInfo {
  name: string;
  path: string;
  size: number;
  modified_at: string;
}

export interface PalDefenderRCONResult {
  command: string;
  output: string;
  entries: string[];
}

export interface PalDefenderCommandCatalogEntry {
  name: string;
  syntax: string;
  description: string;
  category: string;
  transport: 'rest' | 'rcon';
  destructive: boolean;
  reference_url: string;
}

export interface PalDefenderAccessSettings extends PalDefenderAccessSettingsUpdate {
  reload_required: boolean;
  reference_url: string;
}

export interface PalDefenderCatalogReferences {
  pals: string;
  pal_creator: string;
  technology: string;
  passives: string;
  skills: string;
  commands: string;
}

export interface PalDefenderGiveItemsResult {
  Granted: {
    Items: number;
  };
}

export interface TokenResult {
  name: string;
  token: string;
  permissions: string[];
  path: string;
}

export interface PlayerPresenceSession {
  started_at: string;
  ended_at: string;
  duration_seconds: number;
}

export interface Player {
  id: string;
  player_uid?: string;
  steam_id: string;
  nickname: string;
  level: number;
  guild_id?: string;
  guild_name: string;
  is_online: boolean;
  online_source: 'none' | 'rest' | 'paldefender' | 'rest+paldefender';
  online_stale: boolean;
  gm_user_id?: string;
  last_online_time: string;
  x: number;
  y: number;
  z: number;
  ping?: number;
  ip?: string;
  inventory_summary?: Record<string, unknown>;
  note?: string;
  tags?: string[];
  has_annotation?: boolean;
  annotation_updated_at?: string;
  presence_available?: boolean;
  presence_stale?: boolean;
  presence_observed_at?: string;
  presence_online?: boolean;
  session_seconds?: number;
  total_seconds?: number;
  session_started_at?: string;
  last_seen_at?: string;
  last_online_at?: string;
  last_offline_at?: string;
  presence_sessions?: PlayerPresenceSession[];
}

export interface SaveInventorySlot {
  slot: number;
  item_id: string;
  item_name: string;
  count: number;
  durability: number;
}

export interface SaveInventoryContainer {
  container_id: string;
  container_type: string;
  container_name: string;
  owner_type: string;
  owner_id: string;
  slots: SaveInventorySlot[];
}

export interface PlayerDataView {
  scope: 'active' | 'server';
  source_id: string;
  source_kind: 'server' | 'import';
  source_name: string;
  online_overlay: boolean;
}

export interface PlayerListResponse extends EntityListResponse<Player> {
  view: PlayerDataView;
}

export interface SavePlayerDetail {
  player: Player;
  status: SaveIndexStatus;
  view: PlayerDataView;
}

export interface SavePlayerInventory {
  containers: SaveInventoryContainer[];
  status: SaveIndexStatus;
  view: PlayerDataView;
}

export interface PalSkill {
  name: string;
  type: string;
  power: number;
}

export interface WorkSuitability {
  type:
    | 'Handiwork'
    | 'Transport'
    | 'Watering'
    | 'Planting'
    | 'Generating'
    | 'Gathering'
    | 'Lumbering'
    | 'Mining'
    | 'Cooling'
    | 'Farming'
    | 'Medicine'
    | 'Kindling'
    | 'OilExtraction'
    | 'BaseCampBattle'
    | 'Anyone';
  level: number;
}

export interface Pal {
  id: string;
  instance_id?: string;
  character_id?: string;
  species_name?: string;
  name: string;
  nickname?: string;
  level: number;
  rarity: 'Common' | 'Rare' | 'Boss';
  rarity_name?: string;
  owner_player_uid?: string;
  owner_nickname: string;
  owner_steam_id: string;
  guild_id?: string;
  container_id?: string;
  slot_index?: number;
  location_type?: string;
  location_kind?: string;
  terminal_location?: string;
  gender?: 'male' | 'female' | 'wildcard' | string;
  rank?: number;
  stars?: number;
  iv_hp?: number;
  iv_attack?: number;
  iv_defense?: number;
  iv_average?: number;
  equipped_skills?: string[];
  old_owner_uids?: string[];
  on_expedition?: boolean;
  skills: PalSkill[];
  passives?: string[];
  raw_passives?: string[];
  raw_skills?: string[];
  work_suitability: WorkSuitability[];
  health?: number;
  max_health?: number;
  sanity?: number;
  full_stomach?: number;
  is_sick?: boolean;
  status: 'Healthy' | 'Injured' | 'Working' | 'Battling' | 'Dead';
  x: number;
  y: number;
  z: number;
}

export interface BaseStorageSlot {
  slot: number;
  item_id: string;
  item_name: string;
  item_icon: string;
  count: number;
  durability?: number;
}

export interface BaseStorageContainer {
  container_id: string;
  owner_type: string;
  owner_id: string;
  container_type: string;
  container_name: string;
  slots: BaseStorageSlot[];
}

export interface BaseStorageResponse {
  containers: BaseStorageContainer[];
  status: SaveIndexStatus;
}

export interface BaseFeedBox {
  container_id: string;
  container_type: string;
  container_name: string;
  slots: BaseStorageSlot[];
}

export interface BaseFeedItemSummary {
  item_id: string;
  item_name: string;
  item_icon: string;
  count: number;
  box_count: number;
}

export interface BaseFeedBoxSummary {
  box_count: number;
  empty_box_count: number;
  occupied_slots: number;
  total_items: number;
  item_types: number;
}

export interface BaseFeedBoxesResponse {
  base: Base;
  feed_boxes: BaseFeedBox[];
  items: BaseFeedItemSummary[];
  summary: BaseFeedBoxSummary;
  status: SaveIndexStatus;
  source_id: string;
}

export interface BaseWorker {
  instance_id: string;
  character_id: string;
  species_name: string;
  name: string;
  nickname: string;
  level: number;
  gender: string;
  rank: number;
  status: string;
  location_type: string;
  passives: string[];
  raw_passives: string[];
  on_expedition: boolean;
}

export interface BaseWorkerSummary {
  total: number;
  average_level: number;
  max_level: number;
  named_count: number;
  species_count: number;
}

export interface BaseWorkersResponse {
  base: Base;
  workers: BaseWorker[];
  summary: BaseWorkerSummary;
  status: SaveIndexStatus;
  source_id: string;
}

export interface Base {
  id: string;
  name: string;
  raw_name?: string;
  custom_name?: string;
  has_custom_name?: boolean;
  guild_id?: string;
  guild_name: string;
  x: number;
  y: number;
  z: number;
  structures_count: number;
  pals_count: number;
  status: 'Safe' | 'Raid';
  online_members: string[];
  workers?: Array<{
    instance_id: string;
    character_id: string;
    species_name?: string;
    name?: string;
    nickname?: string;
    level?: number;
  }>;
  containers?: string[];
}

export interface GuildMember {
  player_uid: string;
  nickname: string;
  last_online_time?: string;
}

export interface Guild {
  id: string;
  name: string;
  owner_player_uid: string;
  members: GuildMember[];
  base_ids: string[];
  online_member_count: number;
}

export interface GuildMemberDetail {
  id: string;
  player_uid: string;
  steam_id: string;
  nickname: string;
  level: number;
  is_online: boolean;
  last_online_time: string;
  is_owner: boolean;
  note: string;
  tags: string[];
  has_annotation: boolean;
}

export interface GuildBaseDetail {
  id: string;
  name: string;
  raw_name: string;
  custom_name: string;
  has_custom_name: boolean;
  x: number;
  y: number;
  z: number;
  structures_count: number;
  workers_count: number;
  status: string;
}

export interface GuildDetailResponse {
  guild: Guild;
  members: GuildMemberDetail[];
  bases: GuildBaseDetail[];
  status: SaveIndexStatus;
  source_id: string;
}

export interface SaveIndexCounts {
  players: number;
  guilds: number;
  bases: number;
  pals: number;
  containers: number;
  map_entities: number;
}

export interface SaveIndexStatus {
  enabled: boolean;
  state: 'disabled' | 'missing' | 'not_indexed' | 'ready' | 'stale' | 'error' | string;
  stale: boolean;
  source_path: string;
  updated_at: string;
  duration_ms: number;
  error?: string;
  error_code?: string;
  error_detail?: string;
  oodle_available?: boolean;
  warnings: string[];
  counts: SaveIndexCounts;
  parser?: string;
  cache_path?: string;
}

export interface SaveSource {
  id: string;
  name: string;
  kind: 'server' | 'import' | string;
  path?: string;
  active: boolean;
  fingerprint?: string;
  parser_version?: string;
  warnings?: string[];
  indexed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface BreedingCatalogItem {
  id: string;
  name: string;
  raw_name?: string;
}

export interface BreedingPassive extends BreedingCatalogItem {
  supports_surgery: boolean;
  surgery_cost: number;
}

export interface BreedingCatalog {
  version: string;
  pals: BreedingCatalogItem[];
  passives: BreedingPassive[];
  active_skills: BreedingCatalogItem[];
}

export interface BreedingTreeNode {
  type: 'owned' | 'composite_owned' | 'wild' | 'bred' | 'surgery' | string;
  pal_id: string;
  pal_name: string;
  raw_pal_name?: string;
  gender: string;
  passives: string[];
  raw_passives?: string[];
  effort_seconds: number;
  self_effort_seconds: number;
  cost: number;
  probability?: number;
  eggs?: number;
  instance_id?: string;
  owner_player_id?: string;
  container_id?: string;
  slot_index?: number;
  location_type?: string;
  operations?: string[];
  children?: BreedingTreeNode[];
}

export interface BreedingSolveResult {
  pal_id: string;
  pal_name: string;
  raw_pal_name?: string;
  gender: string;
  passives: string[];
  raw_passives?: string[];
  effort_seconds: number;
  breeding_steps: number;
  eggs: number;
  wild_pals: number;
  gold_cost: number;
  tree: BreedingTreeNode;
}

export type MapEntityType = 'player' | 'base' | 'pal' | 'map_object' | string;

export interface MapEntity {
  type: MapEntityType;
  id: string;
  label: string;
  raw_label?: string;
  x: number;
  y: number;
  z: number;
  is_online?: boolean;
  live?: boolean;
  source: 'live' | 'save' | string;
  guild_id?: string;
  guild_name?: string;
  level?: number;
  ping?: number;
  owner_id?: string;
  pals_count?: number;
  location_type?: string;
  status?: string;
}

export interface MapPalTrackingStatus {
  available: boolean;
  real_time: boolean;
  source: string;
  positions: number;
  updated_at: string;
}

export interface MapLiveStatus {
  available: boolean;
  source: string;
  online_players: number;
  refreshed_at: string;
  pals: MapPalTrackingStatus;
}

export interface MapEntitiesResponse {
  entities: MapEntity[];
  status: SaveIndexStatus;
  summary: ListSummary;
  live: MapLiveStatus;
}

export interface ListSummary {
  total: number;
  limit: number;
  offset: number;
  returned: number;
  page: number;
}

export interface EntityListResponse<T> {
  items: T[];
  status: SaveIndexStatus;
  summary: ListSummary;
}

export interface EntityListParams {
  limit?: number;
  offset?: number;
  page?: number;
  q?: string;
  online?: boolean;
  status?: string;
  owner_player_uid?: string;
  guild_id?: string;
  container_id?: string;
}

export interface PalListParams extends EntityListParams {
  min_level?: number;
  min_stars?: number;
  min_iv_average?: number;
  gender?: string;
  location?: string;
  passive?: string;
  sort?: 'level_desc' | 'iv_desc' | 'stars_desc' | 'name_asc';
}

export type UnsupportedActionResult = {
  ok: false;
  unsupported: true;
  message: string;
};

export type IncidentSeverity = 'info' | 'warning' | 'error' | 'critical';
export type IncidentStatus = 'open' | 'acknowledged' | 'resolved';

export interface Incident {
  id: string;
  kind: string;
  severity: IncidentSeverity;
  source: string;
  title: string;
  summary: string;
  status: IncidentStatus;
  occurrences: number;
  first_seen_at: string;
  last_seen_at: string;
  acknowledged_at?: string;
  resolved_at?: string;
  details?: Record<string, unknown>;
  updated_at: string;
}

export interface IncidentEvent {
  id: string;
  incident_id: string;
  type: 'opened' | 'occurred' | 'reopened' | 'acknowledged' | 'resolved' | 'delivery_failed';
  actor?: string;
  message?: string;
  created_at: string;
}

export interface IncidentDelivery {
  id: string;
  incident_id: string;
  event_id: string;
  status: 'pending' | 'delivered' | 'failed';
  attempts: number;
  next_attempt_at?: string;
  delivered_at?: string;
  created_at: string;
  updated_at: string;
}

export interface IncidentListResponse {
  items: Incident[];
  total: number;
  limit: number;
  offset: number;
  summary: Record<IncidentStatus, number>;
  webhook: {
    enabled: boolean;
    target_host?: string;
    signed: boolean;
    timeout_seconds: number;
    max_attempts: number;
  };
}

export interface IncidentDetail {
  incident: Incident;
  events: IncidentEvent[];
  deliveries: IncidentDelivery[];
}
