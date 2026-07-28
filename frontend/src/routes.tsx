import React from 'react';
import {
  Activity, Archive, ClipboardList, Database, Dna, FolderArchive, Globe2, LayoutDashboard,
  Gift, ListTodo, Map as MapIcon, PackageSearch, Puzzle, Settings as SettingsIcon, Shield, Sparkles,
  UserCog, UserX, Users,
} from 'lucide-react';
import type { TranslationKey } from './i18n';

const lazyPage = <T extends Record<string, React.ComponentType>>(loader: () => Promise<T>, exportName: keyof T) =>
  React.lazy(async () => ({ default: (await loader())[exportName] }));

const Backups = lazyPage(() => import('./pages/Backups'), 'Backups');
const AuditLogs = lazyPage(() => import('./pages/AuditLogs'), 'AuditLogs');
const BanList = lazyPage(() => import('./pages/BanList'), 'BanList');
const Bases = lazyPage(() => import('./pages/Bases'), 'Bases');
const BreedingLab = lazyPage(() => import('./pages/BreedingLab'), 'BreedingLab');
const Dashboard = lazyPage(() => import('./pages/Dashboard'), 'Dashboard');
const CommunityServers = lazyPage(() => import('./pages/CommunityServers'), 'CommunityServers');
const Guilds = lazyPage(() => import('./pages/Guilds'), 'Guilds');
const Mods = lazyPage(() => import('./pages/Mods'), 'Mods');
const Monitor = lazyPage(() => import('./pages/Monitor'), 'Monitor');
const LiveMap = lazyPage(() => import('./pages/LiveMap'), 'LiveMap');
const Pals = lazyPage(() => import('./pages/Pals'), 'Pals');
const PalDefenderGM = lazyPage(() => import('./pages/PalDefenderGM'), 'PalDefenderGM');
const Players = lazyPage(() => import('./pages/Players'), 'Players');
const SaveSources = lazyPage(() => import('./pages/SaveSources'), 'SaveSources');
const Inventory = lazyPage(() => import('./pages/Inventory'), 'Inventory');
const StarterGift = lazyPage(() => import('./pages/StarterGift'), 'StarterGift');
const PlayerSummary = lazyPage(() => import('./pages/PlayerSummary'), 'PlayerSummary');
const Security = lazyPage(() => import('./pages/Security'), 'Security');
const Settings = lazyPage(() => import('./pages/Settings'), 'Settings');
const Setup = lazyPage(() => import('./pages/Setup'), 'Setup');
const TaskQueue = lazyPage(() => import('./pages/TaskQueue'), 'TaskQueue');

export type NavGroup = 'setup' | 'workspace' | 'world' | 'system';

export interface AppRoute {
  id: string;
  path: string;
  title: string;
  navLabel: string;
  titleKey: TranslationKey;
  navGroup: NavGroup;
  navVisible?: boolean;
  activePaths?: string[];
  icon: React.ReactNode;
  element: React.ReactElement;
}

export const appRoutes: AppRoute[] = [
  { id: 'setup', path: '/setup', title: '开服向导', navLabel: '开服向导', titleKey: 'route.setup', navGroup: 'setup', icon: <Sparkles size={18} />, element: <Setup /> },
  { id: 'dashboard', path: '/dashboard', title: '服务器总览', navLabel: '服务器总览', titleKey: 'route.dashboard', navGroup: 'workspace', icon: <LayoutDashboard size={18} />, element: <Dashboard /> },
  { id: 'monitor', path: '/monitor', title: '实时监控', navLabel: '实时监控', titleKey: 'route.monitor', navGroup: 'workspace', icon: <Activity size={18} />, element: <Monitor /> },
  { id: 'community-servers', path: '/community-servers', title: '社区服务器', navLabel: '社区服务器', titleKey: 'route.communityServers', navGroup: 'workspace', icon: <Globe2 size={18} />, element: <CommunityServers /> },
  { id: 'player-center', path: '/player-center', title: '玩家中心', navLabel: '玩家中心', titleKey: 'route.playerCenter', navGroup: 'world', activePaths: ['/gm'], icon: <UserCog size={18} />, element: <PalDefenderGM /> },
  { id: 'starter-gift', path: '/starter-gift', title: '新玩家礼包', navLabel: '新玩家礼包', titleKey: 'route.starterGift', navGroup: 'world', icon: <Gift size={18} />, element: <StarterGift /> },
  { id: 'save-sources', path: '/save-sources', title: '存档中心', navLabel: '存档中心', titleKey: 'route.saveSources', navGroup: 'world', icon: <FolderArchive size={18} />, element: <SaveSources /> },
  { id: 'global-inventory', path: '/inventory', title: '库存管理', navLabel: '库存管理', titleKey: 'route.inventory', navGroup: 'world', icon: <PackageSearch size={18} />, element: <Inventory /> },
  { id: 'world-archive', path: '/world', title: '世界档案', navLabel: '世界档案', titleKey: 'route.worldArchive', navGroup: 'world', activePaths: ['/players', '/guilds', '/bases', '/player-summary'], icon: <Database size={18} />, element: <Players /> },
  { id: 'pal-inventory', path: '/pal-inventory', title: '帕鲁仓库', navLabel: '帕鲁仓库', titleKey: 'route.palInventory', navGroup: 'world', activePaths: ['/pals'], icon: <Dna size={18} />, element: <Pals /> },
  { id: 'breeding', path: '/breeding', title: '配种实验室', navLabel: '配种实验室', titleKey: 'route.breeding', navGroup: 'world', icon: <Dna size={18} />, element: <BreedingLab /> },
  { id: 'live-map', path: '/map', title: '实时地图', navLabel: '实时地图', titleKey: 'route.liveMap', navGroup: 'world', icon: <MapIcon size={18} />, element: <LiveMap /> },
  { id: 'mods', path: '/mods', title: 'Mod 管理', navLabel: 'Mod 管理', titleKey: 'route.mods', navGroup: 'world', icon: <Puzzle size={18} />, element: <Mods /> },
  { id: 'backups', path: '/backups', title: '备份与恢复', navLabel: '备份与恢复', titleKey: 'route.backups', navGroup: 'system', icon: <Archive size={18} />, element: <Backups /> },
  { id: 'tasks', path: '/tasks', title: '任务队列', navLabel: '任务队列', titleKey: 'route.tasks', navGroup: 'system', icon: <ListTodo size={18} />, element: <TaskQueue /> },
  { id: 'security', path: '/security', title: '安全防护', navLabel: '安全防护', titleKey: 'route.security', navGroup: 'system', icon: <Shield size={18} />, element: <Security /> },
  { id: 'banlist', path: '/banlist', title: '封禁列表', navLabel: '封禁列表', titleKey: 'route.banlist', navGroup: 'system', icon: <UserX size={18} />, element: <BanList /> },
  { id: 'audit', path: '/audit', title: '操作审计', navLabel: '操作审计', titleKey: 'route.audit', navGroup: 'system', icon: <ClipboardList size={18} />, element: <AuditLogs /> },
  { id: 'settings', path: '/settings', title: '系统设置', navLabel: '系统设置', titleKey: 'route.settings', navGroup: 'system', icon: <SettingsIcon size={18} />, element: <Settings /> },

  // Legacy routes remain directly addressable and are highlighted under their new parent entries.
  { id: 'legacy-gm', path: '/gm', title: '玩家中心', navLabel: '玩家中心', titleKey: 'route.playerCenter', navGroup: 'world', navVisible: false, icon: <UserCog size={18} />, element: <PalDefenderGM /> },
  { id: 'player-summary', path: '/player-summary', title: '世界档案 · 玩家概览', navLabel: '玩家概览', titleKey: 'route.worldArchive', navGroup: 'world', navVisible: false, icon: <ClipboardList size={18} />, element: <PlayerSummary /> },
  { id: 'legacy-players', path: '/players', title: '世界档案 · 玩家', navLabel: '玩家', titleKey: 'route.worldArchive', navGroup: 'world', navVisible: false, icon: <Users size={18} />, element: <Players /> },
  { id: 'legacy-guilds', path: '/guilds', title: '世界档案 · 公会', navLabel: '公会', titleKey: 'route.worldArchive', navGroup: 'world', navVisible: false, icon: <Users size={18} />, element: <Guilds /> },
  { id: 'legacy-bases', path: '/bases', title: '世界档案 · 基地', navLabel: '基地', titleKey: 'route.worldArchive', navGroup: 'world', navVisible: false, icon: <Users size={18} />, element: <Bases /> },
  { id: 'legacy-pals', path: '/pals', title: '帕鲁仓库', navLabel: '帕鲁仓库', titleKey: 'route.palInventory', navGroup: 'world', navVisible: false, icon: <Dna size={18} />, element: <Pals /> },
];

export const navGroups: Array<{ id: NavGroup; title: string; items: AppRoute[] }> = [
  { id: 'setup', title: '开始', items: appRoutes.filter((route) => route.navGroup === 'setup' && route.navVisible !== false) },
  { id: 'workspace', title: '工作台', items: appRoutes.filter((route) => route.navGroup === 'workspace' && route.navVisible !== false) },
  { id: 'world', title: '世界管理', items: appRoutes.filter((route) => route.navGroup === 'world' && route.navVisible !== false) },
  { id: 'system', title: '运维与安全', items: appRoutes.filter((route) => route.navGroup === 'system' && route.navVisible !== false) },
];

export const getRouteMetaByPathname = (pathname: string): AppRoute | undefined => {
  const normalizedPath = pathname.replace(/\/+$/, '') || '/dashboard';
  return appRoutes.find((route) => route.path === normalizedPath || route.activePaths?.includes(normalizedPath));
};
