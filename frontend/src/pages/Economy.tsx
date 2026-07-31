import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Coins, LoaderCircle, RefreshCw, Save, Search, Settings2, WalletCards } from 'lucide-react';
import { economyApi, type EconomyAccount } from '../api/economy';
import { getErrorMessage } from '../api/client';

const number = new Intl.NumberFormat('zh-CN');

export const Economy: React.FC = () => {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [prefix, setPrefix] = useState('!');
  const [dailyPoints, setDailyPoints] = useState(10);
  const [selected, setSelected] = useState<EconomyAccount | null>(null);
  const [delta, setDelta] = useState(0);
  const [reason, setReason] = useState('管理员调整');
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const configQuery = useQuery({ queryKey: ['economy', 'config'], queryFn: economyApi.config });
  const summaryQuery = useQuery({ queryKey: ['economy', 'summary'], queryFn: economyApi.summary });
  const accountsQuery = useQuery({ queryKey: ['economy', 'accounts', search], queryFn: () => economyApi.accounts(search) });
  const ledgerQuery = useQuery({
    queryKey: ['economy', 'ledger', selected?.player_uid],
    queryFn: () => economyApi.ledger(selected?.player_uid || ''),
    enabled: Boolean(selected?.player_uid),
  });

  useEffect(() => {
    if (!configQuery.data) return;
    setPrefix(configQuery.data.command_prefix);
    setDailyPoints(configQuery.data.daily_checkin_points);
  }, [configQuery.data]);

  const saveConfig = useMutation({
    mutationFn: () => economyApi.updateConfig({ command_prefix: prefix, daily_checkin_points: dailyPoints }),
    onSuccess: async () => {
      setNotice({ type: 'success', text: prefix === '' ? '已启用无前缀命令模式。' : `命令前缀已更新为“${prefix}”。` });
      await queryClient.invalidateQueries({ queryKey: ['economy', 'config'] });
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const adjust = useMutation({
    mutationFn: () => {
      if (!selected || delta === 0) throw new Error('请选择玩家并填写非零调整值。');
      return economyApi.adjust(selected.player_uid, delta, reason.trim() || '管理员调整');
    },
    onSuccess: async (result) => {
      setSelected(result.account);
      setDelta(0);
      setNotice({ type: 'success', text: `积分调整完成，当前余额 ${result.account.balance}。` });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['economy', 'summary'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'accounts'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'ledger', result.account.player_uid] }),
      ]);
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const summary = summaryQuery.data;
  const accounts = accountsQuery.data?.items || [];
  const loading = configQuery.isLoading || summaryQuery.isLoading || accountsQuery.isLoading;
  const commandExamples = useMemo(() => ['签到', '积分', '帮助'].map((command) => `${prefix}${command}`), [prefix]);

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['economy'] });
  };

  return (
    <div className="mx-auto flex w-full max-w-[1500px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-bold uppercase tracking-[0.18em] text-sky-600"><Coins size={15} />运营经济</div>
            <h1 className="text-2xl font-black tracking-tight text-slate-900">积分系统</h1>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">统一管理游戏签到、任务、商店和活动使用的积分账户与流水。所有变更都会写入只追加账本。</p>
          </div>
          <button type="button" onClick={() => void refresh()} className="pp-button self-start"><RefreshCw size={14} />刷新</button>
        </div>
        {notice && <div className={`mt-4 rounded-xl border px-4 py-3 text-sm font-semibold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>{notice.text}</div>}
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Metric label="积分账户" value={summary?.accounts || 0} icon={<WalletCards size={18} />} />
        <Metric label="流通积分" value={summary?.total_balance || 0} icon={<Coins size={18} />} />
        <Metric label="今日签到" value={summary?.checkins_today || 0} suffix="人" icon={<Search size={18} />} />
        <Metric label="24小时净发放" value={(summary?.issued_last_24_hours || 0) - (summary?.spent_last_24_hours || 0)} icon={<RefreshCw size={18} />} />
      </section>

      <section className="grid gap-5 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.6fr)]">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="mb-5 flex items-center gap-2"><Settings2 size={18} className="text-sky-500" /><h2 className="font-black text-slate-900">签到与命令配置</h2></div>
          <div className="space-y-4">
            <label className="block">
              <span className="mb-1.5 block text-xs font-bold text-slate-500">游戏命令前缀</span>
              <input value={prefix} onChange={(event) => setPrefix(event.target.value)} maxLength={16} className="pp-input w-full" placeholder="留空表示无前缀" />
              <span className="mt-1.5 block text-xs leading-5 text-slate-400">允许留空；不能包含空格、换行或控制字符，最多16个字符。</span>
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs font-bold text-slate-500">每日签到积分</span>
              <input type="number" min={0} max={1000000} value={dailyPoints} onChange={(event) => setDailyPoints(Number(event.target.value))} className="pp-input w-full" />
            </label>
            <div className="rounded-xl bg-slate-50 p-3 text-xs leading-6 text-slate-500">
              当前示例：{commandExamples.map((example) => <code key={example} className="mr-2 rounded bg-white px-2 py-1 font-bold text-slate-700 shadow-sm">{example}</code>)}
              {prefix === '' && <p className="mt-2 text-amber-700">无前缀模式只识别完整的已知命令，普通聊天不会触发。</p>}
            </div>
            <button type="button" disabled={saveConfig.isPending} onClick={() => saveConfig.mutate()} className="pp-btn pp-btn--primary w-full justify-center">
              {saveConfig.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Save size={15} />}保存配置
            </button>
          </div>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="font-black text-slate-900">积分账户</h2>
            <label className="relative block sm:w-72"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} /><input value={search} onChange={(event) => setSearch(event.target.value)} className="pp-input w-full pl-9" placeholder="昵称 / PlayerUID / SteamID" /></label>
          </div>
          <div className="overflow-x-auto rounded-xl border border-slate-100">
            <table className="min-w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs font-bold text-slate-500"><tr><th className="px-4 py-3">玩家</th><th className="px-4 py-3">PlayerUID</th><th className="px-4 py-3 text-right">余额</th></tr></thead>
              <tbody className="divide-y divide-slate-100">
                {accounts.map((account) => <tr key={account.player_uid} onClick={() => setSelected(account)} className={`cursor-pointer hover:bg-sky-50/60 ${selected?.player_uid === account.player_uid ? 'bg-sky-50' : ''}`}><td className="px-4 py-3 font-bold text-slate-800">{account.nickname || '未命名玩家'}<div className="mt-0.5 text-[11px] font-normal text-slate-400">{account.steam_id || '无 Steam ID'}</div></td><td className="max-w-64 truncate px-4 py-3 font-mono text-xs text-slate-500">{account.player_uid}</td><td className="px-4 py-3 text-right text-base font-black text-slate-900">{number.format(account.balance)}</td></tr>)}
                {!loading && accounts.length === 0 && <tr><td colSpan={3} className="px-4 py-10 text-center text-sm text-slate-400">暂无积分账户。玩家首次签到或查询积分后会自动创建。</td></tr>}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {selected && <section className="grid gap-5 xl:grid-cols-[minmax(0,0.75fr)_minmax(0,1.65fr)]">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="font-black text-slate-900">人工调整</h2>
          <p className="mt-1 text-xs text-slate-400">{selected.nickname || selected.player_uid} · 当前 {number.format(selected.balance)} 积分</p>
          <div className="mt-4 space-y-3">
            <label className="block"><span className="mb-1 block text-xs font-bold text-slate-500">调整值</span><input type="number" value={delta} onChange={(event) => setDelta(Number(event.target.value))} className="pp-input w-full" placeholder="正数增加，负数扣除" /></label>
            <label className="block"><span className="mb-1 block text-xs font-bold text-slate-500">原因</span><input value={reason} onChange={(event) => setReason(event.target.value)} className="pp-input w-full" /></label>
            <button type="button" disabled={adjust.isPending || delta === 0} onClick={() => adjust.mutate()} className="pp-btn pp-btn--primary w-full justify-center">确认调整</button>
          </div>
        </div>
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
          <h2 className="mb-4 font-black text-slate-900">最近流水</h2>
          <div className="space-y-2">
            {(ledgerQuery.data?.items || []).map((entry) => <div key={entry.id} className="flex items-center justify-between rounded-xl border border-slate-100 px-4 py-3"><div><div className="text-sm font-bold text-slate-700">{entry.reason}</div><div className="mt-1 text-[11px] text-slate-400">{entry.actor || '系统'} · {new Date(entry.created_at).toLocaleString()}</div></div><div className="text-right"><div className={`text-base font-black ${entry.delta >= 0 ? 'text-emerald-600' : 'text-rose-600'}`}>{entry.delta >= 0 ? '+' : ''}{number.format(entry.delta)}</div><div className="text-[11px] text-slate-400">余额 {number.format(entry.balance_after)}</div></div></div>)}
            {!ledgerQuery.isLoading && (ledgerQuery.data?.items.length || 0) === 0 && <div className="py-10 text-center text-sm text-slate-400">暂无流水。</div>}
          </div>
        </div>
      </section>}
    </div>
  );
};

const Metric: React.FC<{ label: string; value: number; suffix?: string; icon: React.ReactNode }> = ({ label, value, suffix, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"><div className="mb-4 flex items-center justify-between text-slate-400"><span className="text-xs font-bold uppercase tracking-wider">{label}</span><span className="rounded-lg bg-sky-50 p-2 text-sky-500">{icon}</span></div><div className="text-2xl font-black tracking-tight text-slate-900">{number.format(value)}{suffix && <span className="ml-1 text-sm text-slate-400">{suffix}</span>}</div></div>
);
