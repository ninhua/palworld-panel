import React from 'react';
import { Link } from 'react-router-dom';
import { BookOpen, Boxes, Database, MapPin, Network, ShieldCheck, Sparkles, Sword, UserRound } from 'lucide-react';
import type { Pal, PalDefenderProgression, SaveInventoryContainer } from '../../types';
import { PalIcon } from './PalIcon';

export interface PlayerOverviewModel {
  name: string;
  identifier: string;
  playerUID: string;
  guildName: string;
  level: number;
  online: boolean;
  x: number;
  y: number;
  z: number;
  lastOnline: string;
  hasSaveData: boolean;
  hasLiveData: boolean;
}

export const PlayerOverview: React.FC<{
  player: PlayerOverviewModel;
  progression?: PalDefenderProgression;
  savePals: Pal[];
  saveInventory: SaveInventoryContainer[];
  saveLoading: boolean;
}> = ({ player, progression, savePals, saveInventory, saveLoading }) => {
  const saveItemCount = saveInventory.reduce((total, container) => total + container.slots.reduce((sum, slot) => sum + slot.count, 0), 0);
  const currencies = progression?.Progression.Currencies;

  return (
    <div className="space-y-5 p-4 sm:p-5">
      <div className="flex flex-wrap gap-2">
        <SourceBadge icon={<Database size={13} />} active={player.hasSaveData} label={player.hasSaveData ? '存档索引已匹配' : '没有匹配的存档玩家'} />
        <SourceBadge icon={<ShieldCheck size={13} />} active={player.hasLiveData} label={player.hasLiveData ? 'PalDefender 已匹配' : '仅存档数据'} />
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <OverviewCard icon={<Sparkles size={16} />} label="玩家等级" value={`Lv.${player.level || progression?.Progression.Player.level || 0}`} detail={progression ? `${progression.Progression.Player.exp.toLocaleString()} EXP` : '来自最近存档'} />
        <OverviewCard icon={<Network size={16} />} label="所属公会" value={player.guildName || '无公会'} detail={player.online ? '当前在线' : player.lastOnline || '离线记录'} />
        <OverviewCard icon={<MapPin size={16} />} label="当前位置" value={`${player.x.toFixed(0)}, ${player.y.toFixed(0)}`} detail={`Z ${player.z.toFixed(0)} · ${player.hasLiveData && player.online ? '实时坐标' : '存档坐标'}`} />
        <OverviewCard icon={<Boxes size={16} />} label="存档物品" value={saveLoading ? '读取中' : saveItemCount.toLocaleString()} detail={`${saveInventory.length} 个玩家容器`} />
      </div>

      {currencies && (
        <section className="rounded-2xl border border-slate-100 bg-slate-50/70 p-4">
          <div className="mb-3 flex items-center gap-2 text-xs font-bold text-slate-700"><BookOpen size={15} className="text-violet-500" />成长数据</div>
          <div className="grid gap-3 sm:grid-cols-3">
            <MiniStat label="普通科技点" value={currencies.technologyPoints} />
            <MiniStat label="古代科技点" value={currencies.ancientTechnologyPoints} />
            <MiniStat label="未分配属性点" value={progression.Progression.Player.unusedStatusPoints} />
          </div>
        </section>
      )}

      <section className="rounded-2xl border border-slate-100 bg-white">
        <div className="flex flex-col gap-2 border-b border-slate-100 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h3 className="flex items-center gap-2 text-sm font-bold text-slate-800"><Sword size={15} className="text-sky-500" />玩家帕鲁</h3>
            <p className="mt-1 text-[11px] font-semibold text-slate-400">来自只读存档索引；原始 .sav 不会传到浏览器。</p>
          </div>
          <Link to="/pals" className="text-xs font-bold text-sky-600 hover:text-sky-700">打开完整帕鲁列表</Link>
        </div>
        {savePals.length === 0 ? (
          <div className="px-4 py-10 text-center text-xs font-semibold text-slate-400">没有匹配到该玩家的存档帕鲁</div>
        ) : (
          <div className="grid gap-2 p-3 sm:grid-cols-2 xl:grid-cols-3">
            {savePals.slice(0, 9).map((pal) => (
              <div key={pal.id} className="flex min-w-0 items-center gap-3 rounded-xl border border-slate-100 bg-slate-50/70 p-3">
                <PalIcon characterID={pal.character_id} name={pal.name} className="h-11 w-11 rounded-xl" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-xs font-bold text-slate-700">{pal.nickname || pal.name}</span>
                  <span className="mt-0.5 block truncate font-mono text-[10px] text-slate-400">{pal.character_id || pal.id}</span>
                  <span className="mt-1 flex flex-wrap gap-1">{(pal.passives ?? []).slice(0, 2).map((passive) => <span key={passive} className="max-w-28 truncate rounded-full bg-violet-100 px-1.5 py-0.5 text-[9px] font-bold text-violet-700" title={passive}>{passive}</span>)}{(pal.passives?.length ?? 0) > 2 && <span className="text-[9px] font-bold text-slate-400">+{(pal.passives?.length ?? 0) - 2}</span>}</span>
                </span>
                <span className="flex shrink-0 flex-col items-end gap-1"><span className="rounded-lg bg-white px-2 py-1 text-[10px] font-bold text-slate-500">Lv.{pal.level}</span><span className={`text-[9px] font-bold ${pal.status === 'Dead' ? 'text-rose-600' : pal.status === 'Injured' ? 'text-amber-600' : pal.status === 'Working' ? 'text-sky-600' : 'text-emerald-600'}`}>{pal.status === 'Dead' ? '死亡' : pal.status === 'Injured' ? '异常' : pal.status === 'Working' ? '工作中' : '正常'}</span></span>
              </div>
            ))}
          </div>
        )}
      </section>

      <div className="grid gap-3 sm:grid-cols-3">
        <QuickLink to="/players" icon={<UserRound size={15} />} label="玩家存档" />
        <QuickLink to="/pals" icon={<Sword size={15} />} label="全部帕鲁" />
        <QuickLink to="/map" icon={<MapPin size={15} />} label="世界地图" />
      </div>
    </div>
  );
};

const SourceBadge: React.FC<{ icon: React.ReactNode; active: boolean; label: string }> = ({ icon, active, label }) => (
  <span className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-[10px] font-bold ${active ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}`}>
    {icon}{label}
  </span>
);

const OverviewCard: React.FC<{ icon: React.ReactNode; label: string; value: string; detail: string }> = ({ icon, label, value, detail }) => (
  <div className="rounded-2xl border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-center gap-2 text-[11px] font-bold text-slate-400">{icon}{label}</div>
    <p className="mt-3 truncate text-lg font-black text-slate-800">{value}</p>
    <p className="mt-1 truncate text-[10px] font-semibold text-slate-400">{detail}</p>
  </div>
);

const MiniStat: React.FC<{ label: string; value: number }> = ({ label, value }) => (
  <div className="rounded-xl border border-white bg-white px-4 py-3 shadow-sm">
    <p className="text-[10px] font-bold text-slate-400">{label}</p>
    <p className="mt-1 text-base font-black text-slate-800">{value.toLocaleString()}</p>
  </div>
);

const QuickLink: React.FC<{ to: string; icon: React.ReactNode; label: string }> = ({ to, icon, label }) => (
  <Link to={to} className="flex items-center justify-between rounded-xl border border-slate-200 bg-white px-4 py-3 text-xs font-bold text-slate-600 hover:border-sky-200 hover:bg-sky-50 hover:text-sky-700">
    <span className="flex items-center gap-2">{icon}{label}</span><span aria-hidden="true">→</span>
  </Link>
);
