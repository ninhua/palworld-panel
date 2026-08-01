import { apiClient, handleRequest } from './client';
import { RCON_COMMAND_INSPECTION_TIMEOUT_MS } from './requestTimeouts';
import type {
  PalDefenderAccessSettings,
  PalDefenderAccessSettingsUpdate,
  PalDefenderCatalogReferences,
  PalDefenderCommandCatalogEntry,
  PalDefenderExportedPalTemplateInfo,
  PalDefenderExportPalsResult,
  PalDefenderGiveCustomPalsRequest,
  PalDefenderGivePalsRequest,
  PalDefenderGivePalsResult,
  PalDefenderGivePalTemplatesRequest,
  PalDefenderGivePalTemplatesResult,
  PalDefenderGiveItemsResult,
  PalDefenderGMInventory,
  PalDefenderGMPals,
  PalDefenderGMPlayer,
  PalDefenderGMPlayers,
  PalDefenderGMStatus,
  PalDefenderInventoryContainer,
  PalDefenderInventorySlot,
  PalDefenderItemCatalog,
  PalDefenderItemCatalogEntry,
  PalDefenderItemGrant,
  PalDefenderPalCatalog,
  PalDefenderPalCatalogEntry,
  PalDefenderMessageRequest,
  PalDefenderPunishmentRequest,
  PalDefenderPalTemplate,
  PalDefenderPalTemplateInfo,
  PalDefenderProgression,
  PalDefenderProgressionGrantRequest,
  PalDefenderProgressionGrantResult,
  PalDefenderReleasePalRequest,
  PalDefenderRemoveItemsRequest,
  PalDefenderRCONResult,
  PalDefenderTechnologyRequest,
  PalDefenderTechnologyResult,
  PalDefenderTechnologyCatalog,
  PalDefenderTechnologyCatalogEntry,
  PalDefenderTeleportRequest,
  PalDefenderTechs,
} from '../types';

const emptyLocation = { x: 0, y: 0, z: 0 };

const newGMRequestKey = () => {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) return `gm-${uuid}`;
  return `gm-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

const idempotencyConfig = (key?: string) => ({
  headers: { 'Idempotency-Key': key || newGMRequestKey() },
});

const gmStates = new Set<PalDefenderGMStatus['state']>([
  'ready',
  'not_installed',
  'not_loaded',
  'not_configured',
  'rest_disabled',
  'server_not_running',
  'failed',
]);

const asRecord = (raw: unknown): Record<string, unknown> =>
  raw && typeof raw === 'object' && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};

const mapLocation = (raw: unknown) => {
  const data = asRecord(raw);
  return { x: Number(data.x || 0), y: Number(data.y || 0), z: Number(data.z || 0) };
};

const mapStatus = (raw: unknown): PalDefenderGMStatus => {
  const data = asRecord(raw);
  const version = asRecord(data.version);
  const rawState = String(data.state || 'failed') as PalDefenderGMStatus['state'];
  return {
    configured: Boolean(data.configured),
    available: Boolean(data.available),
    installed: Boolean(data.installed),
    load_verified: Boolean(data.load_verified),
    rest_enabled: Boolean(data.rest_enabled),
    state: gmStates.has(rawState) ? rawState : 'failed',
    error: data.error ? String(data.error) : undefined,
    version:
      Object.keys(version).length > 0
        ? {
            Major: Number(version.Major || 0),
            Minor: Number(version.Minor || 0),
            Patch: Number(version.Patch || 0),
            Build: Number(version.Build || 0),
            Version: String(version.Version || ''),
            VersionLong: String(version.VersionLong || ''),
            Beta: Boolean(version.Beta),
          }
        : undefined,
  };
};

const mapPlayer = (raw: unknown): PalDefenderGMPlayer => {
  const data = asRecord(raw);
  return {
    Name: String(data.Name || '未知玩家'),
    IP: String(data.IP || ''),
    PlayerUID: String(data.PlayerUID || ''),
    UserId: String(data.UserId || ''),
    GuildName: String(data.GuildName || ''),
    GuildUUID: String(data.GuildUUID || ''),
    Status: String(data.Status || 'Unknown'),
    WorldLocation: data.WorldLocation ? mapLocation(data.WorldLocation) : { ...emptyLocation },
    MapLocation: data.MapLocation ? mapLocation(data.MapLocation) : { ...emptyLocation },
  };
};

const mapPlayers = (raw: unknown): PalDefenderGMPlayers => {
  const data = asRecord(raw);
  const meta = asRecord(data.Meta);
  const players = Array.isArray(data.Players) ? data.Players.map(mapPlayer) : [];
  return {
    Meta: {
      PlayerCount: Number(meta.PlayerCount ?? players.length),
      OnlineCount: Number(meta.OnlineCount ?? players.filter((player) => player.Status.toLowerCase() === 'online').length),
    },
    Players: players,
  };
};

const emptyContainer = (): PalDefenderInventoryContainer => ({
  Available: false,
  ContainerID: '',
  UsedSlots: 0,
  MaxSlots: 0,
  FreeSlots: 0,
  Slots: {},
});

const mapContainer = (raw: unknown): PalDefenderInventoryContainer => {
  const data = asRecord(raw);
  const slots: Record<string, PalDefenderInventorySlot> = {};
  for (const [slot, rawValue] of Object.entries(asRecord(data.Slots))) {
    const value = asRecord(rawValue);
    const itemID = String(value.ItemID || '').trim();
    if (!itemID) continue;
    slots[slot] = { ItemID: itemID, Count: Number(value.Count || 0) };
  }
  return {
    Available: Boolean(data.Available),
    ContainerID: String(data.ContainerID || ''),
    UsedSlots: Number(data.UsedSlots ?? Object.keys(slots).length),
    MaxSlots: Number(data.MaxSlots || 0),
    FreeSlots: Number(data.FreeSlots || 0),
    Slots: slots,
  };
};

const mapInventory = (raw: unknown): PalDefenderGMInventory => {
  const data = asRecord(raw);
  const meta = asRecord(data.Meta);
  const inventory = asRecord(data.Inventory);
  return {
    Meta: { PlayerUID: String(meta.PlayerUID || ''), Player: String(meta.Player || '') },
    Inventory: {
      Items: inventory.Items ? mapContainer(inventory.Items) : emptyContainer(),
      KeyItems: inventory.KeyItems ? mapContainer(inventory.KeyItems) : emptyContainer(),
      Weapons: inventory.Weapons ? mapContainer(inventory.Weapons) : emptyContainer(),
      Armor: inventory.Armor ? mapContainer(inventory.Armor) : emptyContainer(),
      Food: inventory.Food ? mapContainer(inventory.Food) : emptyContainer(),
      DropSlot: inventory.DropSlot ? mapContainer(inventory.DropSlot) : emptyContainer(),
    },
  };
};

const mapItemCatalog = (raw: unknown): PalDefenderItemCatalog => {
  const data = asRecord(raw);
  const items = Array.isArray(data.items)
    ? data.items.flatMap((rawItem) => {
        const item = asRecord(rawItem);
        const id = String(item.id || '').trim();
        const name = String(item.name || '').trim();
        if (!id || !name) return [];
        const rawCollaboration = item.collaboration;
        const collaboration: PalDefenderItemCatalogEntry['collaboration'] = rawCollaboration === 'terraria' || rawCollaboration === 'ultrakill' ? rawCollaboration : undefined;
        return [{ id, name, icon: item.icon ? String(item.icon) : undefined, collaboration }];
      })
    : [];
  return { items, returned: Number(data.returned ?? items.length) };
};

const mapPalCatalog = (raw: unknown): PalDefenderPalCatalog => {
  const data = asRecord(raw);
  const items: PalDefenderPalCatalogEntry[] = Array.isArray(data.items)
    ? data.items.flatMap((rawItem) => {
        const item = asRecord(rawItem);
        const id = String(item.id || '').trim();
        const name = String(item.name || '').trim();
        const kind = item.kind === 'standard' || item.kind === 'advanced' ? item.kind : undefined;
        return id && name ? [{ id, name, kind, icon_url: item.icon_url ? String(item.icon_url) : undefined }] : [];
      })
    : [];
  return { items, returned: Number(data.returned ?? items.length) };
};

const mapTechnologyCatalog = (raw: unknown): PalDefenderTechnologyCatalog => {
  const data = asRecord(raw);
  const items: PalDefenderTechnologyCatalogEntry[] = Array.isArray(data.items)
    ? data.items.flatMap((rawItem) => {
        const item = asRecord(rawItem);
        const id = String(item.id || '').trim();
        const name = String(item.name || '').trim();
        if (!id || !name) return [];
        return [{ id, name, level: Number(item.level || 0), category: String(item.category || ''), boss: Boolean(item.boss), ...(item.icon_url ? { icon_url: String(item.icon_url) } : {}) }];
      })
    : [];
  return { items, returned: Number(data.returned ?? items.length) };
};

const mapProgression = (raw: unknown): PalDefenderProgression => {
  const data = asRecord(raw);
  const meta = asRecord(data.Meta);
  const progression = asRecord(data.Progression);
  const player = asRecord(progression.Player);
  const currencies = asRecord(progression.Currencies);
  return {
    Meta: { PlayerUID: String(meta.PlayerUID || ''), Player: String(meta.Player || '') },
    Progression: {
      Player: {
        level: Number(player.level || 0),
        exp: Number(player.exp || 0),
        unusedStatusPoints: Number(player.unusedStatusPoints || 0),
      },
      Currencies: {
        relics: Object.fromEntries(Object.entries(asRecord(currencies.relics)).map(([key, value]) => [key, Number(value || 0)])),
        technologyPoints: Number(currencies.technologyPoints || 0),
        ancientTechnologyPoints: Number(currencies.ancientTechnologyPoints || 0),
      },
      Bosses: asRecord(progression.Bosses),
      Captures: asRecord(progression.Captures),
      Activities: asRecord(progression.Activities),
    },
  };
};

const mapTechs = (raw: unknown): PalDefenderTechs => {
  const data = asRecord(raw);
  const meta = asRecord(data.Meta);
  const techs = asRecord(data.Techs);
  return {
    Meta: {
      PlayerUID: String(meta.PlayerUID || ''), Player: String(meta.Player || ''),
      UnlockedCount: Number(meta.UnlockedCount || 0), LockedCount: Number(meta.LockedCount || 0), TotalCount: Number(meta.TotalCount || 0),
    },
    Techs: { Unlocked: Array.isArray(techs.Unlocked) ? techs.Unlocked.map(String) : [] },
  };
};

const mapPals = (raw: unknown): PalDefenderGMPals => {
  const data = asRecord(raw);
  const meta = asRecord(data.Meta);
  return {
    Meta: {
      PlayerUID: String(meta.PlayerUID || ''), Player: String(meta.Player || ''),
      TeamCount: Number(meta.TeamCount || 0), PalboxCount: Number(meta.PalboxCount || 0), BaseCampCount: Number(meta.BaseCampCount || 0),
    },
    Pals: asRecord(data.Pals),
  };
};

const mapRCONResult = (raw: unknown): PalDefenderRCONResult => {
  const data = asRecord(raw);
  return {
    command: String(data.command || ''),
    output: String(data.output || ''),
    entries: Array.isArray(data.entries) ? data.entries.map(String) : [],
  };
};

const mapTemplateInfo = (raw: unknown): PalDefenderPalTemplateInfo => {
  const data = asRecord(raw);
  return { name: String(data.name || ''), path: String(data.path || ''), size: Number(data.size || 0), modified_at: String(data.modified_at || '') };
};

const mapExportedTemplateInfo = (raw: unknown): PalDefenderExportedPalTemplateInfo => {
  const data = asRecord(raw);
  return {
    player_id: String(data.player_id || ''),
    name: String(data.name || ''),
    path: String(data.path || ''),
    size: Number(data.size || 0),
    modified_at: String(data.modified_at || ''),
  };
};

const mapExportPalsResult = (raw: unknown): PalDefenderExportPalsResult => {
  const data = asRecord(raw);
  const template = asRecord(data.template);
  return {
    player_id: String(data.player_id || ''),
    command: String(data.command || ''),
    output: String(data.output || ''),
    templates: Array.isArray(data.templates) ? data.templates.map(mapExportedTemplateInfo) : [],
    template_info: mapExportedTemplateInfo(data.template_info),
    template: { ...template, PalID: String(template.PalID || '') } as PalDefenderPalTemplate,
  };
};

const mapAccessSettings = (raw: unknown): PalDefenderAccessSettings => {
  const data = asRecord(raw);
  return {
    use_whitelist: Boolean(data.use_whitelist), whitelist_message: String(data.whitelist_message || ''),
    use_admin_whitelist: Boolean(data.use_admin_whitelist), admin_auto_login: Boolean(data.admin_auto_login),
    admin_ips: Array.isArray(data.admin_ips) ? data.admin_ips.map(String) : [],
    reload_required: Boolean(data.reload_required), reference_url: String(data.reference_url || ''),
  };
};

const playerPath = (identifier: string) =>
  `/security/paldefender/gm/players/${encodeURIComponent(identifier)}`;

export const palDefenderGMApi = {
  status: () =>
    handleRequest<unknown, PalDefenderGMStatus>(
      () => apiClient.get('/security/paldefender/gm/status'),
      { configured: false, available: false, installed: false, load_verified: false, rest_enabled: false, state: 'failed' },
      { map: mapStatus, quiet: true, fallbackOnError: false },
    ),

  players: () =>
    handleRequest<unknown, PalDefenderGMPlayers>(
      () => apiClient.get('/security/paldefender/gm/players'),
      { Meta: { PlayerCount: 0, OnlineCount: 0 }, Players: [] },
      { map: mapPlayers, quiet: true, fallbackOnError: false },
    ),

  player: (identifier: string) =>
    handleRequest<unknown, PalDefenderGMPlayer>(
      () => apiClient.get(playerPath(identifier)),
      mapPlayer({ UserId: identifier }),
      { map: mapPlayer, quiet: true, fallbackOnError: false },
    ),

  items: (query = '', limit = 5000) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set('q', query.trim());
    params.set('limit', String(limit));
    return handleRequest<unknown, PalDefenderItemCatalog>(
      () => apiClient.get(`/security/paldefender/gm/items?${params.toString()}`),
      { items: [], returned: 0 },
      { map: mapItemCatalog, quiet: true, fallbackOnError: false },
    );
  },

  inventory: (identifier: string) =>
    handleRequest<unknown, PalDefenderGMInventory>(
      () => apiClient.get(`${playerPath(identifier)}/inventory`),
      { Meta: { PlayerUID: '', Player: identifier }, Inventory: {
        Items: emptyContainer(), KeyItems: emptyContainer(), Weapons: emptyContainer(),
        Armor: emptyContainer(), Food: emptyContainer(), DropSlot: emptyContainer(),
      } },
      { map: mapInventory, quiet: true, fallbackOnError: false },
    ),

  removeItems: (identifier: string, request: PalDefenderRemoveItemsRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post(`${playerPath(identifier)}/items/remove`, request, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  teleport: (identifier: string, request: PalDefenderTeleportRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post(`${playerPath(identifier)}/teleport`, request, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  progression: (identifier: string) =>
    handleRequest<unknown, PalDefenderProgression>(
      () => apiClient.get(`${playerPath(identifier)}/progression`),
      mapProgression({ Meta: { Player: identifier }, Progression: {} }),
      { map: mapProgression, quiet: true, fallbackOnError: false },
    ),

  giveProgression: (identifier: string, request: PalDefenderProgressionGrantRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderProgressionGrantResult>(
      () => apiClient.post(`${playerPath(identifier)}/progression`, request, idempotencyConfig(idempotencyKey)),
      { Granted: {}, Totals: {} },
      { quiet: true, fallbackOnError: false },
    ),

  techs: (identifier: string) =>
    handleRequest<unknown, PalDefenderTechs>(
      () => apiClient.get(`${playerPath(identifier)}/techs`),
      mapTechs({ Meta: { Player: identifier }, Techs: { Unlocked: [] } }),
      { map: mapTechs, quiet: true, fallbackOnError: false },
    ),

  learnTech: (identifier: string, request: PalDefenderTechnologyRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderTechnologyResult>(
      () => apiClient.post(`${playerPath(identifier)}/techs/learn`, request, idempotencyConfig(idempotencyKey)),
      { UnlockedCount: 0, Unlocked: [], Skipped: [] },
      { quiet: true, fallbackOnError: false },
    ),

  forgetTech: (identifier: string, request: PalDefenderTechnologyRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderTechnologyResult>(
      () => apiClient.post(`${playerPath(identifier)}/techs/forget`, request, idempotencyConfig(idempotencyKey)),
      { ForgottenCount: 0, Forgotten: [], Skipped: [] },
      { quiet: true, fallbackOnError: false },
    ),

  pals: (identifier: string) =>
    handleRequest<unknown, PalDefenderGMPals>(
      () => apiClient.get(`${playerPath(identifier)}/pals`),
      mapPals({ Meta: { Player: identifier }, Pals: {} }),
      { map: mapPals, quiet: true, fallbackOnError: false },
    ),

  givePals: (identifier: string, request: PalDefenderGivePalsRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderGivePalsResult>(
      () => apiClient.post(`${playerPath(identifier)}/pals`, request, idempotencyConfig(idempotencyKey)),
      { Granted: { Pals: 0 } },
      { quiet: true, fallbackOnError: false },
    ),

  giveCustomPal: (identifier: string, request: PalDefenderPalTemplate, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderGivePalTemplatesResult>(
      () => apiClient.post(`${playerPath(identifier)}/custom-pal`, request, idempotencyConfig(idempotencyKey)),
      { Granted: { PalTemplates: 0 } },
      { quiet: true, fallbackOnError: false },
    ),

  giveCustomPals: (identifier: string, request: PalDefenderGiveCustomPalsRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderGivePalTemplatesResult>(
      () => apiClient.post(`${playerPath(identifier)}/custom-pals`, request, idempotencyConfig(idempotencyKey)),
      { Granted: { PalTemplates: 0 } },
      { quiet: true, fallbackOnError: false },
    ),

  releasePal: (identifier: string, request: PalDefenderReleasePalRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post(`${playerPath(identifier)}/pals/release`, request, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  givePalTemplates: (identifier: string, request: PalDefenderGivePalTemplatesRequest, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderGivePalTemplatesResult>(
      () => apiClient.post(`${playerPath(identifier)}/pal-templates`, request, idempotencyConfig(idempotencyKey)),
      { Granted: { PalTemplates: 0 } },
      { quiet: true, fallbackOnError: false },
    ),

  exportPals: (identifier: string, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderExportPalsResult>(
      () => apiClient.post(`${playerPath(identifier)}/export-pals`, {}, idempotencyConfig(idempotencyKey)),
      { player_id: identifier, command: '', output: '', templates: [], template_info: { player_id: identifier, name: '', path: '', size: 0, modified_at: '' }, template: { PalID: '' } },
      { map: mapExportPalsResult, quiet: true, fallbackOnError: false },
    ),

  exportedPalTemplates: (identifier: string) =>
    handleRequest<unknown, { player_id: string; templates: PalDefenderExportedPalTemplateInfo[]; reference_url: string }>(
      () => apiClient.get(`${playerPath(identifier)}/exported-pal-templates`),
      { player_id: identifier, templates: [], reference_url: '' },
      {
        map: (raw) => {
          const data = asRecord(raw);
          return {
            player_id: String(data.player_id || identifier),
            templates: Array.isArray(data.templates) ? data.templates.map(mapExportedTemplateInfo) : [],
            reference_url: String(data.reference_url || ''),
          };
        },
        quiet: true,
        fallbackOnError: false,
      },
    ),

  exportedPalTemplate: (identifier: string, name: string) =>
    handleRequest<unknown, PalDefenderPalTemplate>(
      () => apiClient.get(`${playerPath(identifier)}/exported-pal-templates/${encodeURIComponent(name)}`),
      { PalID: '' },
      { quiet: true, fallbackOnError: false },
    ),

  giveItems: (identifier: string, items: PalDefenderItemGrant[], idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderGiveItemsResult>(
      () => apiClient.post(`${playerPath(identifier)}/items`, { Items: items }, idempotencyConfig(idempotencyKey)),
      { Granted: { Items: 0 } },
      { quiet: true, fallbackOnError: false },
    ),

  sendMessage: (identifier: string, request: PalDefenderMessageRequest, idempotencyKey?: string) =>
    handleRequest<unknown, Record<string, unknown>>(
      () => apiClient.post(`${playerPath(identifier)}/message`, request, idempotencyConfig(idempotencyKey)),
      {},
      { quiet: true, fallbackOnError: false },
    ),

  broadcast: (message: string, alert = false, idempotencyKey?: string) =>
    handleRequest<unknown, Record<string, unknown>>(
      () => apiClient.post('/security/paldefender/gm/broadcast', { message, alert }, idempotencyConfig(idempotencyKey)),
      {},
      { quiet: true, fallbackOnError: false },
    ),

  kick: (identifier: string, request: PalDefenderPunishmentRequest = {}, idempotencyKey?: string) =>
    handleRequest<unknown, Record<string, unknown>>(
      () => apiClient.post(`${playerPath(identifier)}/kick`, request, idempotencyConfig(idempotencyKey)),
      {},
      { quiet: true, fallbackOnError: false },
    ),

  ban: (identifier: string, request: PalDefenderPunishmentRequest = {}, idempotencyKey?: string) =>
    handleRequest<unknown, Record<string, unknown>>(
      () => apiClient.post(`${playerPath(identifier)}/ban`, request, idempotencyConfig(idempotencyKey)),
      {},
      { quiet: true, fallbackOnError: false },
    ),

  unban: (identifier: string, request: PalDefenderPunishmentRequest = {}, idempotencyKey?: string) =>
    handleRequest<unknown, Record<string, unknown>>(
      () => apiClient.post(`${playerPath(identifier)}/unban`, request, idempotencyConfig(idempotencyKey)),
      {},
      { quiet: true, fallbackOnError: false },
    ),

  commandCatalog: () =>
    handleRequest<unknown, { commands: PalDefenderCommandCatalogEntry[]; reference_url: string }>(
      () => apiClient.get('/security/paldefender/gm/commands'),
      { commands: [], reference_url: '' },
      { quiet: true, fallbackOnError: false },
    ),

  runtimeCommands: () =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post('/security/paldefender/gm/commands/runtime', {}, { timeout: RCON_COMMAND_INSPECTION_TIMEOUT_MS }),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  technologyCatalog: () =>
    handleRequest<unknown, { catalog: PalDefenderRCONResult; reference_url: string }>(
      () => apiClient.get('/security/paldefender/gm/catalog/technology'),
      { catalog: { command: '', output: '', entries: [] }, reference_url: '' },
      { map: (raw) => { const data = asRecord(raw); return { catalog: mapRCONResult(data.catalog), reference_url: String(data.reference_url || '') }; }, quiet: true, fallbackOnError: false },
    ),

  localTechnologyCatalog: (query = '', limit = 5000) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set('q', query.trim());
    params.set('limit', String(limit));
    return handleRequest<unknown, PalDefenderTechnologyCatalog>(
      () => apiClient.get(`/security/paldefender/gm/catalog/technologies?${params.toString()}`),
      { items: [], returned: 0 },
      { map: mapTechnologyCatalog, quiet: true, fallbackOnError: false },
    );
  },

  palCatalog: (query = '', limit = 5000, advanced = false) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set('q', query.trim());
    params.set('limit', String(limit));
    if (advanced) params.set('advanced', 'true');
    return handleRequest<unknown, PalDefenderPalCatalog>(
      () => apiClient.get(`/security/paldefender/gm/catalog/pals?${params.toString()}`),
      { items: [], returned: 0 },
      { map: mapPalCatalog, quiet: true, fallbackOnError: false },
    );
  },

  passiveCatalog: (query = '', limit = 5000) => {
    const params = new URLSearchParams();
    if (query.trim()) params.set('q', query.trim());
    params.set('limit', String(limit));
    return handleRequest<unknown, PalDefenderPalCatalog>(
      () => apiClient.get(`/security/paldefender/gm/catalog/passives?${params.toString()}`),
      { items: [], returned: 0 },
      { map: mapPalCatalog, quiet: true, fallbackOnError: false },
    );
  },

  skinCatalog: () =>
    handleRequest<unknown, { catalog: PalDefenderRCONResult; reference_url: string }>(
      () => apiClient.get('/security/paldefender/gm/catalog/skins'),
      { catalog: { command: '', output: '', entries: [] }, reference_url: '' },
      { map: (raw) => { const data = asRecord(raw); return { catalog: mapRCONResult(data.catalog), reference_url: String(data.reference_url || '') }; }, quiet: true, fallbackOnError: false },
    ),

  references: () =>
    handleRequest<unknown, PalDefenderCatalogReferences>(
      () => apiClient.get('/security/paldefender/gm/catalog/references'),
      { pals: '', pal_creator: '', technology: '', passives: '', skills: '', commands: '' },
      { map: (raw) => { const data = asRecord(raw); return { pals: String(data.pals || ''), pal_creator: String(data.pal_creator || ''), technology: String(data.technology || ''), passives: String(data.passives || ''), skills: String(data.skills || ''), commands: String(data.commands || '') }; }, quiet: true, fallbackOnError: false },
    ),

  templates: () =>
    handleRequest<unknown, { templates: PalDefenderPalTemplateInfo[]; reference_url: string }>(
      () => apiClient.get('/security/paldefender/gm/pal-templates'),
      { templates: [], reference_url: '' },
      { map: (raw) => { const data = asRecord(raw); return { templates: Array.isArray(data.templates) ? data.templates.map(mapTemplateInfo) : [], reference_url: String(data.reference_url || '') }; }, quiet: true, fallbackOnError: false },
    ),

  template: (name: string) =>
    handleRequest<unknown, PalDefenderPalTemplate>(
      () => apiClient.get(`/security/paldefender/gm/pal-templates/${encodeURIComponent(name)}`),
      { PalID: '' },
      { quiet: true, fallbackOnError: false },
    ),

  putTemplate: (name: string, template: PalDefenderPalTemplate) =>
    handleRequest<unknown, { template: PalDefenderPalTemplateInfo; reload_required: boolean }>(
      () => apiClient.put(`/security/paldefender/gm/pal-templates/${encodeURIComponent(name)}`, template),
      { template: { name, path: '', size: 0, modified_at: '' }, reload_required: false },
      { quiet: true, fallbackOnError: false },
    ),

  deleteTemplate: (name: string) =>
    handleRequest<unknown, { deleted: boolean }>(
      () => apiClient.delete(`/security/paldefender/gm/pal-templates/${encodeURIComponent(name)}`),
      { deleted: false },
      { quiet: true, fallbackOnError: false },
    ),

  accessSettings: () =>
    handleRequest<unknown, PalDefenderAccessSettings>(
      () => apiClient.get('/security/paldefender/access'),
      { use_whitelist: false, whitelist_message: '', use_admin_whitelist: false, admin_auto_login: false, admin_ips: [], reload_required: false, reference_url: '' },
      { map: mapAccessSettings, quiet: true, fallbackOnError: false },
    ),

  putAccessSettings: (request: PalDefenderAccessSettingsUpdate) =>
    handleRequest<unknown, PalDefenderAccessSettings>(
      () => apiClient.put('/security/paldefender/access', request),
      { ...request, reload_required: true, reference_url: '' },
      { map: mapAccessSettings, quiet: true, fallbackOnError: false },
    ),

  whitelist: () =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.get('/security/paldefender/whitelist'),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  whitelistAdd: (identifier: string, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post(`/security/paldefender/whitelist/${encodeURIComponent(identifier)}`, {}, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  whitelistRemove: (identifier: string, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.delete(`/security/paldefender/whitelist/${encodeURIComponent(identifier)}`, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),

  toggleAdmin: (identifier: string, idempotencyKey?: string) =>
    handleRequest<unknown, PalDefenderRCONResult>(
      () => apiClient.post(`/security/paldefender/admins/${encodeURIComponent(identifier)}/toggle`, {}, idempotencyConfig(idempotencyKey)),
      { command: '', output: '', entries: [] },
      { map: mapRCONResult, quiet: true, fallbackOnError: false },
    ),
};
