import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from './client';
import { palDefenderGMApi } from './paldefenderGM';
import { RCON_COMMAND_INSPECTION_TIMEOUT_MS } from './requestTimeouts';

describe('PalDefender GM API', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('maps official uppercase player and inventory payloads', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockResolvedValueOnce({
      status: 200,
      data: {
        ok: true,
        data: {
          Meta: { PlayerCount: 1, OnlineCount: 1 },
          Players: [{ Name: 'Builder', UserId: 'steam_1', PlayerUID: 'uid_1', Status: 'Online', MapLocation: { x: 4, y: 5, z: 6 } }],
        },
      },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: {
        ok: true,
        data: {
          Meta: { Player: 'steam_1', PlayerUID: 'uid_1' },
          Inventory: { Items: { Available: true, UsedSlots: 1, MaxSlots: 42, FreeSlots: 41, Slots: { 0: { ItemID: 'Money', Count: 25 } } } },
        },
      },
    });

    const players = await palDefenderGMApi.players();
    const inventory = await palDefenderGMApi.inventory('steam_1');

    expect(players.Players[0]).toMatchObject({ Name: 'Builder', UserId: 'steam_1', MapLocation: { x: 4, y: 5, z: 6 } });
    expect(inventory.Inventory.Items.Slots['0']).toEqual({ ItemID: 'Money', Count: 25 });
    expect(inventory.Inventory.Armor.Slots).toEqual({});
    expect(get).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam_1/inventory');
  });

  it('sends validated GM write payloads to encoded player routes', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: { Success: true, Granted: { Items: 5 } } } });

    await palDefenderGMApi.giveItems('steam/user', [{ ItemID: 'Money', Count: 5 }], 'gm-request-001');
    await palDefenderGMApi.sendMessage('steam/user', { SendType: 'PlayerChat', Message: 'hello' }, 'gm-request-002');
    await palDefenderGMApi.ban('steam/user', { Reason: 'abuse', IP: true }, 'gm-request-003');
    await palDefenderGMApi.removeItems('steam/user', { Items: [{ ItemID: 'Money', Count: 2 }] }, 'gm-request-004');
    await palDefenderGMApi.teleport('steam/user', { Mode: 'player', TargetPlayer: 'gdk_2' }, 'gm-request-005');
    await palDefenderGMApi.releasePal('steam/user', { PalID: 'Anubis', Level: 50, Gender: 'male', Rank: 4 }, 'gm-request-006');

    expect(post).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/players/steam%2Fuser/items', { Items: [{ ItemID: 'Money', Count: 5 }] }, { headers: { 'Idempotency-Key': 'gm-request-001' } });
    expect(post).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam%2Fuser/message', { SendType: 'PlayerChat', Message: 'hello' }, { headers: { 'Idempotency-Key': 'gm-request-002' } });
    expect(post).toHaveBeenNthCalledWith(3, '/security/paldefender/gm/players/steam%2Fuser/ban', { Reason: 'abuse', IP: true }, { headers: { 'Idempotency-Key': 'gm-request-003' } });
    expect(post).toHaveBeenNthCalledWith(4, '/security/paldefender/gm/players/steam%2Fuser/items/remove', { Items: [{ ItemID: 'Money', Count: 2 }] }, { headers: { 'Idempotency-Key': 'gm-request-004' } });
    expect(post).toHaveBeenNthCalledWith(5, '/security/paldefender/gm/players/steam%2Fuser/teleport', { Mode: 'player', TargetPlayer: 'gdk_2' }, { headers: { 'Idempotency-Key': 'gm-request-005' } });
    expect(post).toHaveBeenNthCalledWith(6, '/security/paldefender/gm/players/steam%2Fuser/pals/release', { PalID: 'Anubis', Level: 50, Gender: 'male', Rank: 4 }, { headers: { 'Idempotency-Key': 'gm-request-006' } });
  });

  it('maps structured readiness state and player detail', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { configured: true, available: false, installed: true, load_verified: false, rest_enabled: true, state: 'not_loaded' } },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { Name: 'Builder', UserId: 'steam_1', PlayerUID: 'uid_1', Status: 'Online' } },
    });

    const status = await palDefenderGMApi.status();
    const player = await palDefenderGMApi.player('steam_1');

    expect(status).toMatchObject({ installed: true, load_verified: false, rest_enabled: true, state: 'not_loaded' });
    expect(player).toMatchObject({ Name: 'Builder', UserId: 'steam_1', PlayerUID: 'uid_1' });
    expect(get).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam_1');
  });

  it('maps the local item catalog and requests the full bounded list', async () => {
    const get = vi.spyOn(apiClient, 'get').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { items: [{ id: 'Money', name: '金币', icon: 'money' }], returned: 1 } },
    });

    const catalog = await palDefenderGMApi.items('', 5000);

    expect(catalog).toEqual({ items: [{ id: 'Money', name: '金币', icon: 'money' }], returned: 1 });
    expect(get).toHaveBeenCalledWith('/security/paldefender/gm/items?limit=5000');
  });

  it('maps local Pal, passive, and technology catalogs', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { items: [{ id: 'Anubis', name: '阿努比斯' }], returned: 1 } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { items: [{ id: 'CraftSpeed_up3', name: '卓绝技艺' }], returned: 1 } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { items: [{ id: 'Workbench', name: '原始的作业台', level: 1, category: '建筑', boss: false, icon_url: 'https://cdn.example/workbench.webp' }], returned: 1 } } });

    expect(await palDefenderGMApi.palCatalog('', 5000)).toEqual({ items: [{ id: 'Anubis', name: '阿努比斯' }], returned: 1 });
    expect(await palDefenderGMApi.passiveCatalog('', 5000)).toEqual({ items: [{ id: 'CraftSpeed_up3', name: '卓绝技艺' }], returned: 1 });
    expect(await palDefenderGMApi.localTechnologyCatalog('', 5000)).toEqual({ items: [{ id: 'Workbench', name: '原始的作业台', level: 1, category: '建筑', boss: false, icon_url: 'https://cdn.example/workbench.webp' }], returned: 1 });
    expect(get).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/catalog/pals?limit=5000');
    expect(get).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/catalog/passives?limit=5000');
    expect(get).toHaveBeenNthCalledWith(3, '/security/paldefender/gm/catalog/technologies?limit=5000');
  });

  it('reads progression and technology state and sends typed grants', async () => {
    const get = vi.spyOn(apiClient, 'get');
    const post = vi.spyOn(apiClient, 'post');
    get.mockResolvedValueOnce({
      status: 200,
      data: {
        ok: true,
        data: {
          Meta: { PlayerUID: 'uid_1', Player: 'steam/user' },
          Progression: {
            Player: { level: 42, exp: 1200, unusedStatusPoints: 3 },
            Currencies: { relics: { LifmunkEffigy: 8 }, technologyPoints: 12, ancientTechnologyPoints: 4 },
          },
        },
      },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: {
        ok: true,
        data: {
          Meta: { PlayerUID: 'uid_1', Player: 'steam/user', UnlockedCount: 2, LockedCount: 1, TotalCount: 3 },
          Techs: { Unlocked: ['Technology_1', 'Technology_2'] },
        },
      },
    });
    post.mockResolvedValue({ status: 200, data: { ok: true, data: { Skipped: [] } } });

    const progression = await palDefenderGMApi.progression('steam/user');
    const techs = await palDefenderGMApi.techs('steam/user');
    await palDefenderGMApi.giveProgression('steam/user', { TechnologyPoints: 10, AncientTechnologyPoints: 2 }, 'gm-progress');
    await palDefenderGMApi.learnTech('steam/user', { Technology: ['Technology_3'] }, 'gm-learn');
    await palDefenderGMApi.forgetTech('steam/user', { Technology: 'Technology_2' }, 'gm-forget');

    expect(progression.Progression.Currencies).toEqual({ relics: { LifmunkEffigy: 8 }, technologyPoints: 12, ancientTechnologyPoints: 4 });
    expect(techs).toMatchObject({ Meta: { UnlockedCount: 2, TotalCount: 3 }, Techs: { Unlocked: ['Technology_1', 'Technology_2'] } });
    expect(get).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/players/steam%2Fuser/progression');
    expect(get).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam%2Fuser/techs');
    expect(post).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/players/steam%2Fuser/progression', { TechnologyPoints: 10, AncientTechnologyPoints: 2 }, { headers: { 'Idempotency-Key': 'gm-progress' } });
    expect(post).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam%2Fuser/techs/learn', { Technology: ['Technology_3'] }, { headers: { 'Idempotency-Key': 'gm-learn' } });
    expect(post).toHaveBeenNthCalledWith(3, '/security/paldefender/gm/players/steam%2Fuser/techs/forget', { Technology: 'Technology_2' }, { headers: { 'Idempotency-Key': 'gm-forget' } });
  });

  it('supports direct pals, editable templates, and template grants', async () => {
    const get = vi.spyOn(apiClient, 'get');
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: { Granted: { Pals: 1 } } } });
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { template: { name: 'combat_fox', path: 'PalTemplates/combat_fox.json', size: 256, modified_at: '2026-07-16T00:00:00Z' }, reload_required: false } },
    });
    const del = vi.spyOn(apiClient, 'delete').mockResolvedValue({ status: 200, data: { ok: true, data: { deleted: true } } });
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { Meta: { Player: 'steam_1', PlayerUID: 'uid_1', TeamCount: 1, PalboxCount: 2, BaseCampCount: 3 }, Pals: { Team: [] } } },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { templates: [{ name: 'combat_fox', path: 'PalTemplates/combat_fox.json', size: 256, modified_at: '2026-07-16T00:00:00Z' }], reference_url: 'https://paldeck.cc/creator' } },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { player_id: 'steam_1', templates: [{ player_id: 'steam_1', name: 'Existing Fox.json', path: 'Pals/Exported/steam_1/Existing Fox.json', size: 384, modified_at: '2026-07-16T00:01:00Z' }], reference_url: 'commands' } },
    });
    get.mockResolvedValueOnce({
      status: 200,
      data: { ok: true, data: { PalID: 'Foxparks', Level: 48, IVs: { Health: 90 }, Passives: ['Legend'] } },
    });

    const pals = await palDefenderGMApi.pals('steam_1');
    const templates = await palDefenderGMApi.templates();
    const exported = await palDefenderGMApi.exportedPalTemplates('steam_1');
    const exportedTemplate = await palDefenderGMApi.exportedPalTemplate('steam_1', 'Existing Fox.json');
    await palDefenderGMApi.givePals('steam_1', { Pals: [{ PalID: 'Foxparks', Level: 50 }] }, 'gm-pal');
    await palDefenderGMApi.giveCustomPal('steam_1', { PalID: 'Foxparks', Level: 50, Passives: ['Legend'], IsAwakening: true }, 'gm-custom-pal');
    await palDefenderGMApi.givePalTemplates('steam_1', { PalTemplates: ['combat_fox.json'] }, 'gm-template');
    await palDefenderGMApi.putTemplate('combat_fox', { PalID: 'Foxparks', Level: 50, IVs: { Health: 100 }, Passives: ['Legend'] });
    await palDefenderGMApi.deleteTemplate('combat_fox');

    expect(pals.Meta).toMatchObject({ TeamCount: 1, PalboxCount: 2, BaseCampCount: 3 });
    expect(templates.templates[0]).toMatchObject({ name: 'combat_fox', size: 256 });
    expect(exported.templates[0]).toMatchObject({ player_id: 'steam_1', name: 'Existing Fox.json', size: 384 });
    expect(exportedTemplate).toMatchObject({ PalID: 'Foxparks', Level: 48, IVs: { Health: 90 } });
    expect(get).toHaveBeenNthCalledWith(3, '/security/paldefender/gm/players/steam_1/exported-pal-templates');
    expect(get).toHaveBeenNthCalledWith(4, '/security/paldefender/gm/players/steam_1/exported-pal-templates/Existing%20Fox.json');
    expect(post).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/players/steam_1/pals', { Pals: [{ PalID: 'Foxparks', Level: 50 }] }, { headers: { 'Idempotency-Key': 'gm-pal' } });
    expect(post).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam_1/custom-pal', { PalID: 'Foxparks', Level: 50, Passives: ['Legend'], IsAwakening: true }, { headers: { 'Idempotency-Key': 'gm-custom-pal' } });
    expect(post).toHaveBeenNthCalledWith(3, '/security/paldefender/gm/players/steam_1/pal-templates', { PalTemplates: ['combat_fox.json'] }, { headers: { 'Idempotency-Key': 'gm-template' } });
    expect(put).toHaveBeenCalledWith('/security/paldefender/gm/pal-templates/combat_fox', { PalID: 'Foxparks', Level: 50, IVs: { Health: 100 }, Passives: ['Legend'] });
    expect(del).toHaveBeenCalledWith('/security/paldefender/gm/pal-templates/combat_fox');
  });

  it('sends one custom template batch and maps the confirmed export', async () => {
	const post = vi.spyOn(apiClient, 'post');
	post.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { Granted: { PalTemplates: 20 } } } });
	post.mockResolvedValueOnce({
		status: 200,
		data: { ok: true, data: {
			player_id: 'steam_1', command: '/exportpals steam_1', output: 'accepted',
			templates: [{ player_id: 'steam_1', name: 'Anubis new.json', path: 'exported/Anubis new.json', size: 128, modified_at: '2026-07-23T00:00:00Z' }],
			template_info: { player_id: 'steam_1', name: 'Anubis new.json', path: 'exported/Anubis new.json', size: 128, modified_at: '2026-07-23T00:00:00Z' },
			template: { PalID: 'Anubis', Level: 50, Passives: ['MutationPal_Immortal'] },
		} },
	});

	const batch = await palDefenderGMApi.giveCustomPals('steam_1', { Template: { PalID: 'Anubis', Level: 50 }, Count: 20 }, 'gm-custom-batch');
	const exported = await palDefenderGMApi.exportPals('steam_1', 'gm-export');

	expect(batch.Granted.PalTemplates).toBe(20);
	expect(exported).toMatchObject({ player_id: 'steam_1', template_info: { name: 'Anubis new.json' }, template: { PalID: 'Anubis' } });
	expect(post).toHaveBeenNthCalledWith(1, '/security/paldefender/gm/players/steam_1/custom-pals', { Template: { PalID: 'Anubis', Level: 50 }, Count: 20 }, { headers: { 'Idempotency-Key': 'gm-custom-batch' } });
	expect(post).toHaveBeenNthCalledWith(2, '/security/paldefender/gm/players/steam_1/export-pals', {}, { headers: { 'Idempotency-Key': 'gm-export' } });
  });

  it('maps access settings, references, and typed RCON operations', async () => {
    const get = vi.spyOn(apiClient, 'get');
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({ status: 200, data: { ok: true, data: { command: '/setadmin steam/user', output: 'ok', entries: ['steam/user'] } } });
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { use_whitelist: true, whitelist_message: 'apply first', use_admin_whitelist: true, admin_auto_login: false, admin_ips: ['127.0.0.1'], reload_required: true, reference_url: 'commands' } },
    });
    const del = vi.spyOn(apiClient, 'delete').mockResolvedValue({ status: 200, data: { ok: true, data: { command: '/whitelist_remove steam/user', output: 'removed', entries: [] } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { use_whitelist: false, whitelist_message: '', use_admin_whitelist: false, admin_auto_login: false, admin_ips: [], reload_required: false, reference_url: 'commands' } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { command: '/whitelist_get', output: 'steam_1\nsteam_2', entries: ['steam_1', 'steam_2'] } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { catalog: { command: '/gettechids', output: 'Technology_1', entries: ['Technology_1'] }, reference_url: 'https://paldeck.cc/technology' } } });
    get.mockResolvedValueOnce({ status: 200, data: { ok: true, data: { pals: 'pals', pal_creator: 'creator', technology: 'tech', passives: 'passives', skills: 'skills', commands: 'commands' } } });

    const access = await palDefenderGMApi.accessSettings();
    const whitelist = await palDefenderGMApi.whitelist();
    const technology = await palDefenderGMApi.technologyCatalog();
    const references = await palDefenderGMApi.references();
    await palDefenderGMApi.putAccessSettings({ use_whitelist: true, whitelist_message: 'apply first', use_admin_whitelist: true, admin_auto_login: false, admin_ips: ['127.0.0.1'] });
    await palDefenderGMApi.whitelistAdd('steam/user', 'gm-whitelist-add');
    await palDefenderGMApi.whitelistRemove('steam/user', 'gm-whitelist-remove');
    await palDefenderGMApi.toggleAdmin('steam/user', 'gm-admin');

    expect(access).toMatchObject({ use_whitelist: false, reload_required: false });
    expect(whitelist.entries).toEqual(['steam_1', 'steam_2']);
    expect(technology).toEqual({ catalog: { command: '/gettechids', output: 'Technology_1', entries: ['Technology_1'] }, reference_url: 'https://paldeck.cc/technology' });
    expect(references).toEqual({ pals: 'pals', pal_creator: 'creator', technology: 'tech', passives: 'passives', skills: 'skills', commands: 'commands' });
    expect(put).toHaveBeenCalledWith('/security/paldefender/access', { use_whitelist: true, whitelist_message: 'apply first', use_admin_whitelist: true, admin_auto_login: false, admin_ips: ['127.0.0.1'] });
    expect(post).toHaveBeenNthCalledWith(1, '/security/paldefender/whitelist/steam%2Fuser', {}, { headers: { 'Idempotency-Key': 'gm-whitelist-add' } });
    expect(del).toHaveBeenCalledWith('/security/paldefender/whitelist/steam%2Fuser', { headers: { 'Idempotency-Key': 'gm-whitelist-remove' } });
    expect(post).toHaveBeenNthCalledWith(2, '/security/paldefender/admins/steam%2Fuser/toggle', {}, { headers: { 'Idempotency-Key': 'gm-admin' } });
  });

  it('runs runtime RCON inspection as an audited operation with an extended timeout', async () => {
    const post = vi.spyOn(apiClient, 'post').mockResolvedValue({
      status: 200,
      data: { ok: true, data: { command: '/getrconcmds', output: 'tp\ndelitems', entries: [] } },
    });

    const result = await palDefenderGMApi.runtimeCommands();

    expect(result).toMatchObject({ command: '/getrconcmds', output: 'tp\ndelitems' });
    expect(post).toHaveBeenCalledWith(
      '/security/paldefender/gm/commands/runtime',
      {},
      { timeout: RCON_COMMAND_INSPECTION_TIMEOUT_MS },
    );
  });
});
