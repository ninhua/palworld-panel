import React from 'react';
import { Database, PackageSearch } from 'lucide-react';
import type { PalDefenderItemCatalogEntry, SaveInventoryContainer } from '../../types';

export const SaveInventoryPanel: React.FC<{
  containers: SaveInventoryContainer[];
  catalog: PalDefenderItemCatalogEntry[];
  loading: boolean;
}> = ({ containers, catalog, loading }) => {
  const items = containers.flatMap((container) => container.slots.map((slot) => ({ ...slot, container_id: container.container_id, container_name: container.container_name })));
  const catalogMap = new Map(catalog.map((item) => [item.id.toLowerCase(), item]));

  return (
    <section className="border-t border-slate-100 p-4 sm:p-5">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><Database size={15} className="text-violet-500" />存档背包快照</h3>
          <p className="mt-1 text-[11px] font-semibold text-slate-400">只显示 sav-cli 已解析的 JSON 字段，不读取或输出原始二进制内容。</p>
        </div>
        <span className="text-[11px] font-bold text-slate-400">{containers.length} 个容器 · {items.length} 个已用槽位 · {items.reduce((total, item) => total + item.count, 0).toLocaleString()} 件物品</span>
      </div>
      <div className="mt-3 max-h-[32rem] overflow-auto overscroll-contain rounded-xl border border-slate-200">
        <table className="w-full min-w-[560px] text-left">
          <thead className="bg-slate-50 text-[10px] font-bold text-slate-400"><tr><th className="px-4 py-2.5">物品</th><th className="px-4 py-2.5">容器</th><th className="px-4 py-2.5">槽位</th><th className="px-4 py-2.5 text-right">数量</th></tr></thead>
          <tbody className="divide-y divide-slate-100">
            {loading ? (
              <tr><td colSpan={4} className="px-4 py-10 text-center text-xs font-semibold text-slate-400">正在读取存档背包索引...</td></tr>
            ) : items.length === 0 ? (
              <tr><td colSpan={4} className="px-4 py-10 text-center text-xs font-semibold text-slate-400"><PackageSearch className="mr-2 inline" size={15} />没有解析到玩家背包槽位</td></tr>
            ) : items.map((slot) => {
              const entry = catalogMap.get(slot.item_id.toLowerCase());
              return (
                <tr key={`${slot.container_id}:${slot.slot}:${slot.item_id}`}>
                  <td className="px-4 py-3">
                    <div className="flex min-w-0 items-center gap-2">
                      {entry?.icon ? <img src={`/assets/items/${encodeURIComponent(entry.icon)}.webp`} alt="" className="h-8 w-8 shrink-0 object-contain" /> : <span className="h-8 w-8 shrink-0 rounded-lg bg-slate-100" />}
                      <span className="min-w-0"><span className="block truncate text-xs font-bold text-slate-700">{slot.item_name || entry?.name || slot.item_id}</span><span className="block truncate font-mono text-[10px] text-slate-400">{slot.item_id}</span></span>
                    </div>
                  </td>
                  <td className="px-4 py-3"><span className="block text-[10px] font-bold text-slate-600">{slot.container_name || '玩家背包'}</span><span className="block max-w-48 truncate font-mono text-[9px] text-slate-400" title={slot.container_id}>{slot.container_id}</span></td>
                  <td className="px-4 py-3 text-xs font-semibold text-slate-500">{slot.slot}</td>
                  <td className="px-4 py-3 text-right text-xs font-bold text-slate-700">{slot.count.toLocaleString()}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
};
