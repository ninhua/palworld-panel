import React, { useMemo, useState } from 'react';
import { Boxes, LoaderCircle, PackagePlus, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import type { PalDefenderInventory, PalDefenderInventoryContainer, PalDefenderItemCatalogEntry, PalDefenderItemGrant } from '../../types';

type ContainerKey = keyof PalDefenderInventory;
type GrantRow = PalDefenderItemGrant & { key: number; name?: string; icon?: string };

const containerOptions: Array<{ key: ContainerKey; label: string }> = [
  { key: 'Items', label: '物品' }, { key: 'KeyItems', label: '关键物品' }, { key: 'Weapons', label: '武器' },
  { key: 'Armor', label: '防具' }, { key: 'Food', label: '食物' }, { key: 'DropSlot', label: '掉落槽' },
];

let grantKey = 1;

export const ItemWorkspace: React.FC<{
  catalog: PalDefenderItemCatalogEntry[];
  inventory?: PalDefenderInventory;
  inventoryLoading: boolean;
  canWrite: boolean;
  online: boolean;
  busy: boolean;
  pending: string;
  onRefresh: () => void;
  onGive: (items: PalDefenderItemGrant[]) => Promise<void>;
  onAdjust: (itemID: string, currentCount: number, targetCount: number) => Promise<boolean>;
}> = ({ catalog, inventory, inventoryLoading, canWrite, online, busy, pending, onRefresh, onGive, onAdjust }) => {
  const [search, setSearch] = useState('');
  const [catalogMode, setCatalogMode] = useState<'all' | 'collaboration' | 'terraria' | 'ultrakill'>('all');
  const [selected, setSelected] = useState<PalDefenderItemCatalogEntry | null>(null);
  const [count, setCount] = useState('1');
  const [rows, setRows] = useState<GrantRow[]>([]);
  const [containerKey, setContainerKey] = useState<ContainerKey>('Items');
  const [adjustment, setAdjustment] = useState<{ itemID: string; name: string; current: number } | null>(null);
  const [targetCount, setTargetCount] = useState('0');

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return catalog.filter((item) => {
      if (catalogMode === 'collaboration' && !item.collaboration) return false;
      if ((catalogMode === 'terraria' || catalogMode === 'ultrakill') && item.collaboration !== catalogMode) return false;
      return !needle || item.id.toLowerCase().includes(needle) || item.name.toLowerCase().includes(needle);
    }).slice(0, needle ? 80 : 48);
  }, [catalog, catalogMode, search]);

  const addSelected = () => {
    const amount = Number(count);
    if (!selected || !Number.isInteger(amount) || amount <= 0) return;
    setRows((current) => {
      const existing = current.find((row) => row.ItemID === selected.id);
      if (existing) return current.map((row) => row.key === existing.key ? { ...row, Count: row.Count + amount } : row);
      return [...current, { key: grantKey++, ItemID: selected.id, Count: amount, name: selected.name, icon: selected.icon }];
    });
  };

  const submit = async () => {
    if (rows.length === 0) return;
    await onGive(rows.map(({ ItemID, Count }) => ({ ItemID, Count })));
    setRows([]);
  };

  const container: PalDefenderInventoryContainer | undefined = inventory?.[containerKey];
  const slots = container ? Object.entries(container.Slots).sort(([left], [right]) => Number(left) - Number(right)) : [];
  const catalogMap = new Map(catalog.map((item) => [item.id.toLowerCase(), item]));
  const inventoryTotals = new Map<string, number>();
  if (inventory) {
    for (const inventoryContainer of Object.values(inventory)) {
      for (const slot of Object.values(inventoryContainer.Slots)) {
        inventoryTotals.set(slot.ItemID.toLowerCase(), (inventoryTotals.get(slot.ItemID.toLowerCase()) || 0) + slot.Count);
      }
    }
  }

  const openAdjustment = (itemID: string, name: string) => {
    const current = inventoryTotals.get(itemID.toLowerCase()) || 0;
    setAdjustment({ itemID, name, current });
    setTargetCount(String(current));
  };

  const submitAdjustment = async () => {
    if (!adjustment) return;
    const target = Number(targetCount);
    if (!Number.isInteger(target) || target < 0) return;
    if (await onAdjust(adjustment.itemID, adjustment.current, target)) setAdjustment(null);
  };

  return (
    <div className="grid gap-5 p-4 sm:p-5 xl:grid-cols-[minmax(0,1.05fr)_minmax(380px,0.95fr)]">
      <section className="min-w-0 rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
        <div><h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><PackagePlus size={16} className="text-sky-500" />选择并发放物品</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">先选物品，再加入发放列表；所有操作都发送给当前玩家。</p></div>
        <label className="relative mt-4 block">
          <span className="sr-only">搜索物品</span><Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
          <input aria-label="搜索物品" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索中文名或 ItemID" className="w-full rounded-xl border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none" />
        </label>
        <div className="mt-3 flex flex-wrap gap-1.5" aria-label="物品目录筛选">
          {([['all', '全部物品'], ['collaboration', '联动物品'], ['terraria', '泰拉瑞亚'], ['ultrakill', 'ULTRAKILL']] as const).map(([mode, label]) => (
            <button type="button" key={mode} onClick={() => setCatalogMode(mode)} aria-pressed={catalogMode === mode} className={`rounded-lg border px-2.5 py-1.5 text-[10px] font-bold transition-colors ${catalogMode === mode ? 'border-sky-200 bg-sky-50 text-sky-700' : 'border-slate-200 bg-white text-slate-500 hover:bg-slate-50'}`}>{label}</button>
          ))}
        </div>
        <div className="mt-3 grid max-h-80 grid-cols-2 gap-2 overflow-y-auto pr-1 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-3 2xl:grid-cols-4">
          {filtered.map((item) => (
            <button type="button" key={item.id} onClick={() => setSelected(item)} aria-pressed={selected?.id === item.id} className={`flex min-w-0 flex-col items-center rounded-xl border p-2 text-center transition-colors ${selected?.id === item.id ? 'border-sky-300 bg-sky-50 ring-2 ring-sky-100' : 'border-slate-100 hover:border-slate-200 hover:bg-slate-50'}`}>
              {item.icon ? <img src={`/assets/items/${encodeURIComponent(item.icon)}.webp`} alt={`${item.name}图标`} className="h-11 w-11 object-contain" /> : <span className="h-11 w-11 rounded-lg bg-slate-100" />}
              <span className="mt-1.5 w-full truncate text-[11px] font-bold text-slate-700">{item.name}</span><span className="mt-0.5 w-full truncate font-mono text-[9px] text-slate-400">{item.id}</span>
            </button>
          ))}
        </div>
        <div className="mt-4 flex flex-col gap-3 rounded-xl border border-slate-100 bg-slate-50 p-3 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">{selected ? <><p className="truncate text-xs font-bold text-slate-700">{selected.name}</p><p className="truncate font-mono text-[10px] text-slate-400">{selected.id}</p></> : <p className="text-xs font-semibold text-slate-400">请从上方选择一个物品</p>}</div>
          <input aria-label="发放数量" type="number" min={1} value={count} onChange={(event) => setCount(event.target.value)} className="w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-semibold text-slate-700 sm:w-24" />
          <button type="button" onClick={addSelected} disabled={!selected || Number(count) <= 0} className="inline-flex items-center justify-center gap-2 rounded-lg border border-sky-200 bg-white px-3 py-2 text-xs font-bold text-sky-700 disabled:opacity-40"><Plus size={13} />加入列表</button>
        </div>
        <div className="mt-4 space-y-2">
          {rows.length === 0 ? <div className="rounded-xl border border-dashed border-slate-200 px-4 py-7 text-center text-xs font-semibold text-slate-400">发放列表为空</div> : rows.map((row) => <div key={row.key} className="flex items-center gap-3 rounded-xl border border-slate-100 px-3 py-2.5">{row.icon ? <img src={`/assets/items/${encodeURIComponent(row.icon)}.webp`} alt="" className="h-9 w-9 object-contain" /> : <span className="h-9 w-9 rounded-lg bg-slate-100" />}<span className="min-w-0 flex-1"><span className="block truncate text-xs font-bold text-slate-700">{row.name || row.ItemID}</span><span className="block truncate font-mono text-[9px] text-slate-400">{row.ItemID}</span></span><input aria-label={`${row.name || row.ItemID} 数量`} type="number" min={1} value={row.Count} onChange={(event) => setRows((current) => current.map((item) => item.key === row.key ? { ...item, Count: Number(event.target.value) } : item))} className="w-20 rounded-lg border border-slate-200 px-2 py-1.5 text-xs font-semibold" /><button type="button" aria-label={`删除 ${row.name || row.ItemID}`} onClick={() => setRows((current) => current.filter((item) => item.key !== row.key))} className="text-slate-400 hover:text-rose-600"><Trash2 size={14} /></button></div>)}
        </div>
        <button type="button" onClick={() => void submit()} disabled={!canWrite || !online || busy || rows.length === 0} className="mt-4 inline-flex items-center gap-2 rounded-xl bg-slate-900 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'give-items' ? <LoaderCircle size={14} className="animate-spin" /> : <PackagePlus size={14} />}确认发放</button>
        {!online && <p className="mt-2 text-[10px] font-semibold text-amber-700">物品发放需要玩家在线；离线时仍可查看右侧存档背包。</p>}
      </section>

      <section className="min-w-0 rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
        <div className="flex items-center justify-between gap-3"><div><h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><Boxes size={16} className="text-violet-500" />PalDefender 实时背包</h3><p className="mt-1 text-[11px] font-semibold text-slate-400">玩家在线时读取六个实时容器。</p></div><button type="button" onClick={onRefresh} title="刷新实时背包" className="flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 text-slate-500"><RefreshCw size={13} className={inventoryLoading ? 'animate-spin' : ''} /></button></div>
        <div className="mt-4 flex flex-wrap items-center gap-2"><select aria-label="背包容器" value={containerKey} onChange={(event) => setContainerKey(event.target.value as ContainerKey)} className="rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs font-bold text-slate-700">{containerOptions.map((option) => <option key={option.key} value={option.key}>{option.label}</option>)}</select>{container && <span className="text-[10px] font-bold text-slate-400">{container.UsedSlots}/{container.MaxSlots} 槽 · {container.FreeSlots} 空闲</span>}</div>
        <div className="mt-3 max-h-[560px] overflow-y-auto rounded-xl border border-slate-200"><table className="w-full min-w-[430px] text-left"><thead className="sticky top-0 bg-slate-50 text-[10px] font-bold text-slate-400"><tr><th className="px-3 py-2.5">物品</th><th className="px-3 py-2.5">槽位</th><th className="px-3 py-2.5 text-right">数量</th><th className="px-3 py-2.5 text-right">操作</th></tr></thead><tbody className="divide-y divide-slate-100">{inventoryLoading && slots.length === 0 ? <tr><td colSpan={4} className="px-3 py-10 text-center text-xs font-semibold text-slate-400">正在读取实时背包...</td></tr> : slots.length === 0 ? <tr><td colSpan={4} className="px-3 py-10 text-center text-xs font-semibold text-slate-400">该容器暂无实时数据</td></tr> : slots.map(([slot, item]) => { const entry = catalogMap.get(item.ItemID.toLowerCase()); return <tr key={slot}><td className="px-3 py-3"><div className="flex min-w-0 items-center gap-2">{entry?.icon ? <img src={`/assets/items/${encodeURIComponent(entry.icon)}.webp`} alt="" className="h-8 w-8 object-contain" /> : <span className="h-8 w-8 rounded-lg bg-slate-100" />}<span className="min-w-0"><span className="block truncate text-xs font-bold text-slate-700">{entry?.name || item.ItemID}</span><span className="block truncate font-mono text-[9px] text-slate-400">{item.ItemID}</span></span></div></td><td className="px-3 py-3 font-mono text-[10px] text-slate-400">{slot}</td><td className="px-3 py-3 text-right text-xs font-bold text-slate-700">{item.Count.toLocaleString()}</td><td className="px-3 py-3 text-right"><button type="button" onClick={() => openAdjustment(item.ItemID, entry?.name || item.ItemID)} disabled={!canWrite || !online || busy} className="rounded-lg border border-slate-200 px-2.5 py-1.5 text-[10px] font-bold text-slate-600 disabled:opacity-40">调整总量</button></td></tr>; })}</tbody></table></div>
      </section>
      {adjustment && <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/45 p-4" role="presentation"><section role="dialog" aria-modal="true" aria-labelledby="adjust-item-title" className="w-full max-w-md rounded-2xl bg-white p-5 shadow-2xl"><h3 id="adjust-item-title" className="text-base font-black text-slate-900">调整 {adjustment.name} 总量</h3><p className="mt-2 text-[11px] font-semibold leading-5 text-slate-500">当前跨全部背包容器共 {adjustment.current.toLocaleString()} 件。增加会调用发放，减少会调用 PalDefender 物品移除。</p><label className="mt-4 block text-xs font-bold text-slate-600">目标总量<input aria-label="目标物品总量" type="number" min={0} value={targetCount} onChange={(event) => setTargetCount(event.target.value)} className="mt-1.5 w-full rounded-xl border border-slate-200 px-3 py-2.5 text-xs font-semibold" /></label><div className="mt-5 flex justify-end gap-2"><button type="button" onClick={() => setAdjustment(null)} disabled={busy} className="rounded-xl border border-slate-200 px-4 py-2.5 text-xs font-bold text-slate-600 disabled:opacity-40">取消</button><button type="button" onClick={() => void submitAdjustment()} disabled={busy || !Number.isInteger(Number(targetCount)) || Number(targetCount) < 0 || Number(targetCount) === adjustment.current} className="inline-flex items-center gap-2 rounded-xl bg-slate-900 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">{pending === 'adjust-item' && <LoaderCircle size={14} className="animate-spin" />}确认调整</button></div></section></div>}
    </div>
  );
};
