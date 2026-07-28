import { apiClient, handleRequest } from './client';

export interface StarterGiftItem { item_id: string; count: number }
export interface StarterGiftCatalogItem { id: string; name: string; icon?: string }
export interface StarterGiftConfig {
  enabled: boolean;
  items: StarterGiftItem[];
  pal_templates: string[];
  item_batch_size: number;
  template_batch_size: number;
  batch_delay_ms: number;
}
export interface StarterGiftGrantEvent {
  at: string;
  phase: string;
  level: string;
  message: string;
  item_from?: number;
  item_to?: number;
  template_from?: number;
  template_to?: number;
}
export interface StarterGiftGrant {
  player_id: string;
  player_uid?: string;
  steam_id?: string;
  nickname?: string;
  status: 'pending' | 'running' | 'success' | 'failed' | string;
  phase?: string;
  detection_source?: string;
  detection_reason?: string;
  manual?: boolean;
  resolved_player_id?: string;
  next_item: number;
  next_template: number;
  item_total: number;
  template_total: number;
  progress_percent: number;
  attempts: number;
  first_seen_at: string;
  updated_at: string;
  completed_at?: string;
  last_error?: string;
  events: StarterGiftGrantEvent[];
}
export interface StarterGiftPlayerDecision {
  player_id: string;
  player_uid?: string;
  steam_id?: string;
  nickname?: string;
  online: boolean;
  seen: boolean;
  rearmed: boolean;
  is_new: boolean;
  eligible: boolean;
  decision: string;
  reason: string;
  grant_status?: string;
  grant_phase?: string;
  evidence: string[];
  last_update_at?: string;
}
export interface PalTemplateInfo {
  name: string;
  pal_id?: string;
  pal_name?: string;
  english_name?: string;
  category?: string;
  usage_category?: string;
  overall_grade?: string;
  index_names: string[];
  classification_tags: string[];
  graduation_pal?: boolean;
  graduation_uses: string[];
  current_graduation_use?: boolean;
  transitional?: boolean;
  passive_names: string[];
  night_work_mode?: string;
  nickname?: string;
  level?: number;
  modified_at?: string;
  size?: number;
  parse_error?: string;
}
export interface PalTemplateIndexInfo { name: string; label: string; count: number }
export interface StarterGiftScope { id: string; world_id: string; world_path?: string }
export interface StarterGiftSnapshot {
  scope: StarterGiftScope;
  config: StarterGiftConfig;
  grants: StarterGiftGrant[];
  players: StarterGiftPlayerDecision[];
  worker_running: boolean;
  templates: PalTemplateInfo[];
  template_indexes: PalTemplateIndexInfo[];
  item_catalog: StarterGiftCatalogItem[];
  template_error?: string;
  players_error?: string;
  save_index_state?: string;
}

export type StarterGiftPlayerAction = 'retry' | 'supplement' | 'reissue' | 'next_login';

const emptyConfig: StarterGiftConfig = {
  enabled: false,
  items: [],
  pal_templates: [],
  item_batch_size: 20,
  template_batch_size: 5,
  batch_delay_ms: 500,
};

const strings = (value: unknown) => (Array.isArray(value) ? value : []).map(String).filter(Boolean);
const optionalString = (value: unknown) => value ? String(value) : undefined;

const mapSnapshot = (raw: unknown): StarterGiftSnapshot => {
  const data = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
  const config = (data.config && typeof data.config === 'object' ? data.config : {}) as Record<string, unknown>;
  const scope = (data.scope && typeof data.scope === 'object' ? data.scope : {}) as Record<string, unknown>;
  return {
    scope: {
      id: String(scope.id || ''),
      world_id: String(scope.world_id || ''),
      world_path: optionalString(scope.world_path),
    },
    config: {
      enabled: Boolean(config.enabled),
      items: (Array.isArray(config.items) ? config.items : []).map((rawItem) => {
        const item = (rawItem && typeof rawItem === 'object' ? rawItem : {}) as Record<string, unknown>;
        return { item_id: String(item.item_id || ''), count: Number(item.count || 0) };
      }),
      pal_templates: strings(config.pal_templates),
      item_batch_size: Number(config.item_batch_size || emptyConfig.item_batch_size),
      template_batch_size: Number(config.template_batch_size || emptyConfig.template_batch_size),
      batch_delay_ms: Number(config.batch_delay_ms || emptyConfig.batch_delay_ms),
    },
    grants: (Array.isArray(data.grants) ? data.grants : []).map((rawGrant) => {
      const grant = (rawGrant && typeof rawGrant === 'object' ? rawGrant : {}) as Record<string, unknown>;
      return {
        player_id: String(grant.player_id || ''),
        player_uid: optionalString(grant.player_uid),
        steam_id: optionalString(grant.steam_id),
        nickname: optionalString(grant.nickname),
        status: String(grant.status || 'pending'),
        phase: optionalString(grant.phase),
        detection_source: optionalString(grant.detection_source),
        detection_reason: optionalString(grant.detection_reason),
        manual: Boolean(grant.manual),
        resolved_player_id: optionalString(grant.resolved_player_id),
        next_item: Number(grant.next_item || 0),
        next_template: Number(grant.next_template || 0),
        item_total: Number(grant.item_total || 0),
        template_total: Number(grant.template_total || 0),
        progress_percent: Number(grant.progress_percent || 0),
        attempts: Number(grant.attempts || 0),
        first_seen_at: String(grant.first_seen_at || ''),
        updated_at: String(grant.updated_at || ''),
        completed_at: optionalString(grant.completed_at),
        last_error: optionalString(grant.last_error),
        events: (Array.isArray(grant.events) ? grant.events : []).map((rawEvent) => {
          const event = (rawEvent && typeof rawEvent === 'object' ? rawEvent : {}) as Record<string, unknown>;
          return {
            at: String(event.at || ''), phase: String(event.phase || ''), level: String(event.level || ''), message: String(event.message || ''),
            item_from: event.item_from == null ? undefined : Number(event.item_from), item_to: event.item_to == null ? undefined : Number(event.item_to),
            template_from: event.template_from == null ? undefined : Number(event.template_from), template_to: event.template_to == null ? undefined : Number(event.template_to),
          };
        }),
      };
    }),
    players: (Array.isArray(data.players) ? data.players : []).map((rawPlayer) => {
      const player = (rawPlayer && typeof rawPlayer === 'object' ? rawPlayer : {}) as Record<string, unknown>;
      return {
        player_id: String(player.player_id || ''), player_uid: optionalString(player.player_uid), steam_id: optionalString(player.steam_id), nickname: optionalString(player.nickname),
        online: Boolean(player.online), seen: Boolean(player.seen), rearmed: Boolean(player.rearmed), is_new: Boolean(player.is_new), eligible: Boolean(player.eligible),
        decision: String(player.decision || ''), reason: String(player.reason || ''), grant_status: optionalString(player.grant_status), grant_phase: optionalString(player.grant_phase),
        evidence: strings(player.evidence), last_update_at: optionalString(player.last_update_at),
      };
    }).filter((player) => player.player_id),
    worker_running: Boolean(data.worker_running),
    templates: (Array.isArray(data.templates) ? data.templates : []).map((rawTemplate) => {
      const template = (rawTemplate && typeof rawTemplate === 'object' ? rawTemplate : {}) as Record<string, unknown>;
      return {
        name: String(template.name || ''), pal_id: optionalString(template.pal_id), pal_name: optionalString(template.pal_name), english_name: optionalString(template.english_name),
        category: optionalString(template.category), usage_category: optionalString(template.usage_category), overall_grade: optionalString(template.overall_grade),
        index_names: strings(template.index_names), classification_tags: strings(template.classification_tags), graduation_pal: Boolean(template.graduation_pal),
        graduation_uses: strings(template.graduation_uses), current_graduation_use: Boolean(template.current_graduation_use), transitional: Boolean(template.transitional),
        passive_names: strings(template.passive_names), night_work_mode: optionalString(template.night_work_mode), nickname: optionalString(template.nickname),
        level: template.level == null ? undefined : Number(template.level), modified_at: optionalString(template.modified_at), size: template.size == null ? undefined : Number(template.size), parse_error: optionalString(template.parse_error),
      };
    }).filter((template) => template.name),
    template_indexes: (Array.isArray(data.template_indexes) ? data.template_indexes : []).map((rawIndex) => {
      const index = (rawIndex && typeof rawIndex === 'object' ? rawIndex : {}) as Record<string, unknown>;
      return { name: String(index.name || ''), label: String(index.label || index.name || ''), count: Number(index.count || 0) };
    }).filter((index) => index.name),
    item_catalog: (Array.isArray(data.item_catalog) ? data.item_catalog : []).map((rawItem) => {
      const item = (rawItem && typeof rawItem === 'object' ? rawItem : {}) as Record<string, unknown>;
      return { id: String(item.id || ''), name: String(item.name || item.id || ''), icon: optionalString(item.icon) };
    }).filter((item) => item.id),
    template_error: optionalString(data.template_error),
    players_error: optionalString(data.players_error),
    save_index_state: optionalString(data.save_index_state),
  };
};

export const starterGiftApi = {
  get: () => handleRequest<unknown, StarterGiftSnapshot>(
    () => apiClient.get('/security/paldefender/starter-gift'), mapSnapshot({}), { fallbackOnError: false, map: mapSnapshot },
  ),
  save: (config: StarterGiftConfig) => handleRequest<unknown, StarterGiftSnapshot>(
    () => apiClient.put('/security/paldefender/starter-gift', config), mapSnapshot({}), { fallbackOnError: false, map: mapSnapshot },
  ),
  action: (id: string, action: StarterGiftPlayerAction) => handleRequest(
    () => apiClient.post(`/security/paldefender/starter-gift/grants/${encodeURIComponent(id)}/retry`, { action }), {}, { fallbackOnError: false },
  ),
  retry: (id: string) => starterGiftApi.action(id, 'retry'),
  forget: (id: string) => handleRequest(
    () => apiClient.delete(`/security/paldefender/starter-gift/grants/${encodeURIComponent(id)}`), {}, { fallbackOnError: false },
  ),
};
