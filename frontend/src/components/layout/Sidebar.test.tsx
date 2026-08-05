import { describe, expect, it } from 'vitest';
import { appRoutes } from '../../routes';
import { groupedSidebarRouteIDs, sidebarGroups } from './Sidebar';

describe('sidebar navigation coverage', () => {
  it('contains every visible route exactly once', () => {
    const grouped = groupedSidebarRouteIDs();
    const counts = new Map<string, number>();
    for (const routeID of grouped) counts.set(routeID, (counts.get(routeID) || 0) + 1);

    const visibleRouteIDs = appRoutes
      .filter((route) => route.navVisible !== false)
      .map((route) => route.id)
      .sort();
    const groupedVisibleRouteIDs = grouped
      .filter((routeID) => visibleRouteIDs.includes(routeID))
      .sort();

    expect(groupedVisibleRouteIDs).toEqual(visibleRouteIDs);
    expect([...counts.entries()].filter(([, count]) => count > 1)).toEqual([]);
  });

  it('does not reference routes that do not exist', () => {
    const routeIDs = new Set(appRoutes.map((route) => route.id));
    const missing = sidebarGroups
      .flatMap((group) => group.entries)
      .flatMap((entry) => entry.routeIDs)
      .filter((routeID) => !routeIDs.has(routeID));
    expect(missing).toEqual([]);
  });
});
