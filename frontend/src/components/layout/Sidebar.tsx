import React, { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { NavLink, useLocation } from 'react-router-dom';
import { ChevronDown, LogOut, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { useServerStore } from '../../store/useServerStore';
import { appRoutes, type AppRoute } from '../../routes';
import { appConfig } from '../../config/defaults';
import { useI18n, type TranslationKey } from '../../i18n';
import { patchInfoApi } from '../../api/patchInfo';

interface SidebarProps {
  mobile?: boolean;
  onNavigate?: () => void;
}

const formatUptime = (seconds?: number) => {
  if (!seconds) return '—';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  return days > 0 ? `${days}d ${hours}h` : `${hours}h`;
};

interface SidebarEntry {
  id: string;
  labelKey?: TranslationKey;
  label?: string;
  routeIDs: string[];
}

const sidebarGroups: Array<{ id: string; titleKey: TranslationKey; entries: SidebarEntry[] }> = [
  { id: 'setup', titleKey: 'nav.setupGroup', entries: [{ id: 'setup', labelKey: 'nav.setup', routeIDs: ['setup'] }] },
  {
    id: 'workspace',
    titleKey: 'nav.workspaceGroup',
    entries: [
      { id: 'server-center', labelKey: 'nav.serverCenter', routeIDs: ['dashboard', 'monitor'] },
      { id: 'community-servers', labelKey: 'route.communityServers', routeIDs: ['community-servers'] },
    ],
  },
  {
    id: 'world',
    titleKey: 'nav.worldGroup',
    entries: [
      { id: 'players-world', labelKey: 'nav.playersWorld', routeIDs: ['player-center', 'starter-gift', 'world-archive'] },
      { id: 'economy', label: '积分系统', routeIDs: ['economy', 'operations-tasks'] },
      { id: 'saves-breeding', labelKey: 'nav.saveTools', routeIDs: ['save-sources', 'save-history', 'global-inventory', 'pal-inventory', 'breeding', 'live-map'] },
      { id: 'mods', labelKey: 'nav.mods', routeIDs: ['mods'] },
    ],
  },
  {
    id: 'system',
    titleKey: 'nav.systemGroup',
    entries: [
      { id: 'backup-tasks', labelKey: 'nav.backupTasks', routeIDs: ['backups', 'tasks'] },
      { id: 'security-audit', labelKey: 'nav.securityAudit', routeIDs: ['security', 'banlist', 'audit', 'incidents'] },
      { id: 'diagnostics', labelKey: 'route.diagnostics', routeIDs: ['diagnostics'] },
      { id: 'settings', labelKey: 'nav.settings', routeIDs: ['settings'] },
    ],
  },
];

const routesByID = new Map(appRoutes.map((route) => [route.id, route]));

const routeIsActive = (route: AppRoute, currentPath: string) =>
  currentPath === route.path || Boolean(route.activePaths?.includes(currentPath));

export const Sidebar: React.FC<SidebarProps> = ({ mobile = false, onNavigate }) => {
  const { t } = useI18n();
  const { isSidebarCollapsed, setIsSidebarCollapsed, session, logout, status, metrics } = useServerStore();
  const location = useLocation();
  const collapsed = !mobile && isSidebarCollapsed;
  const currentPath = location.pathname.replace(/\/+$/, '') || '/';
  const running = status?.status === 'running';
  const online = metrics?.current_players || 0;
  const capacity = metrics?.max_players || 32;
  const [expandedEntries, setExpandedEntries] = useState<Set<string>>(new Set());
  const patchInfo = useQuery({
    queryKey: ['patch-info'],
    queryFn: patchInfoApi.get,
    staleTime: Infinity,
    retry: false,
  });
  const panelVersion = patchInfo.data?.patch.version || '';
  const labelForEntry = (entry: SidebarEntry) => entry.label || (entry.labelKey ? t(entry.labelKey) : entry.id);
  const labelForRoute = (route: AppRoute) => route.titleKey ? t(route.titleKey) : route.navLabel;


  useEffect(() => {
    const activeEntry = sidebarGroups
      .flatMap((group) => group.entries)
      .find((entry) => entry.routeIDs.some((routeID) => {
        const route = routesByID.get(routeID);
        return route ? routeIsActive(route, currentPath) : false;
      }));
    if (activeEntry && activeEntry.routeIDs.length > 1) {
      setExpandedEntries((current) => {
        if (current.has(activeEntry.id)) return current;
        const next = new Set(current);
        next.add(activeEntry.id);
        return next;
      });
    }
  }, [currentPath]);

  const toggleEntry = (entryID: string) => {
    setExpandedEntries((current) => {
      const next = new Set(current);
      if (next.has(entryID)) next.delete(entryID);
      else next.add(entryID);
      return next;
    });
  };

  return (
    <aside className={`pp-rail ${mobile ? 'is-mobile' : ''} ${collapsed ? 'is-collapsed' : ''}`}>
      <div className="pp-brandmark">
        <NavLink to="/dashboard" onClick={onNavigate} className="pp-brandmark__logo" aria-label={t('nav.overview', { brand: appConfig.brand })}>
          <img src="/brand/palpanel-mark.svg" alt="" width="32" height="32" />
        </NavLink>
        <NavLink to="/dashboard" onClick={onNavigate} className="pp-brandmark__copy" aria-hidden={collapsed}>
          <span className="pp-brandmark__name">{appConfig.brand}</span>
          <span className="pp-brandmark__tag">{panelVersion ? `v${panelVersion}` : 'version pending'} · server control</span>
        </NavLink>
        {!mobile && (
          <button
            type="button"
            onClick={() => setIsSidebarCollapsed(!collapsed)}
            className="pp-rail-toggle"
            title={collapsed ? t('nav.expand') : t('nav.collapse')}
            aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
          >
            {collapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />}
          </button>
        )}
      </div>

      <nav className="pp-nav" aria-label={t('nav.main')}>
        {sidebarGroups.map((group) => (
          <React.Fragment key={group.id}>
            <div className="pp-nav__group">{t(group.titleKey)}</div>
            {group.entries.map((entry) => {
              const routes = entry.routeIDs.map((routeID) => routesByID.get(routeID)).filter((route): route is AppRoute => Boolean(route));
              const primaryRoute = routes[0];
              if (!primaryRoute) return null;
              const active = routes.some((route) => routeIsActive(route, currentPath));
              const entryLabel = labelForEntry(entry);
              if (routes.length === 1 || collapsed) {
                return (
                  <NavLink
                    key={entry.id}
                    to={primaryRoute.path}
                    onClick={onNavigate}
                    title={collapsed ? entryLabel : undefined}
                    className={`pp-nav__item ${active ? 'is-active' : ''}`}
                  >
                    {primaryRoute.icon}
                    <span className="pp-nav__label pp-truncate">{entryLabel}</span>
                  </NavLink>
                );
              }

              const expanded = expandedEntries.has(entry.id);
              return (
                <React.Fragment key={entry.id}>
                  <button
                    type="button"
                    onClick={() => toggleEntry(entry.id)}
                    className={`pp-nav__item pp-nav__item--cluster ${active ? 'is-active' : ''}`}
                    aria-expanded={expanded}
                  >
                    {primaryRoute.icon}
                    <span className="pp-nav__label pp-truncate">{entryLabel}</span>
                    <ChevronDown size={14} className={`pp-nav__chevron ${expanded ? 'is-open' : ''}`} />
                  </button>
                  {expanded && (
                    <div className="pp-nav__sub">
                      {routes.map((route) => (
                        <NavLink
                          key={route.id}
                          to={route.path}
                          onClick={onNavigate}
                          className={`pp-nav__subitem ${routeIsActive(route, currentPath) ? 'is-active' : ''}`}
                        >
                          {labelForRoute(route)}
                        </NavLink>
                      ))}
                    </div>
                  )}
                </React.Fragment>
              );
            })}
          </React.Fragment>
        ))}
      </nav>

      {!collapsed && (
        <section className="pp-heartbeat" aria-label={t('nav.serverHeartbeat')}>
          <div className="pp-heartbeat__top">
            <span className={`pp-pulse ${running ? '' : 'is-down'}`} />
            <span className="pp-heartbeat__label">{t('nav.serverHeartbeat')}</span>
            <span className="pp-heartbeat__state">{running ? t('nav.running') : t('nav.stopped')}</span>
          </div>
          <div className="pp-heartbeat__meta">
            <span><strong>{online}/{capacity}</strong>{t('nav.online')}</span>
            <span className="pp-right"><strong>{formatUptime(metrics?.uptime)}</strong>{t('nav.uptime')}</span>
          </div>
        </section>
      )}

      <div className="pp-rail-account">
        <span className="pp-rail-account__avatar">{(session?.name || appConfig.brand).slice(0, 2).toUpperCase()}</span>
        <span className="pp-rail-account__copy">
          <strong>{session?.name || 'Admin'}</strong>
          <span>{t('nav.adminSession')}</span>
        </span>
        <button type="button" onClick={() => void logout()} className="pp-rail-account__logout" title={t('nav.logout')} aria-label={t('nav.logout')}>
          <LogOut size={15} />
        </button>
      </div>
    </aside>
  );
};
