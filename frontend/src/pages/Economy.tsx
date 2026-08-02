import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleAlert,
  Coins,
  Database,
  FileSearch,
  LoaderCircle,
  RefreshCw,
  Save,
  Search,
  Settings2,
  Upload,
  WalletCards,
} from 'lucide-react';
import { economyApi, type EconomyAccount, type LegacyAstrBotPreview } from '../api/economy';
import { getErrorMessage } from '../api/client';

const number = new Intl.NumberFormat('zh-CN');

export const Economy: React.FC = () => {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [prefix, setPrefix] = useState('!');
  const [allowBareCommands, setAllowBareCommands] = useState(true);
  const [dailyPoints, setDailyPoints] = useState(10);
  const [streakEnabled, setStreakEnabled] = useState(true);
  const [streakBonusPerDay, setStreakBonusPerDay] = useState(2);
  const [streakMaxDays, setStreakMaxDays] = useState(7);
  const [cycleDays, setCycleDays] = useState(7);
  const [cycleBonus, setCycleBonus] = useState(10);
  const [checkinAliases, setCheckinAliases] = useState('签到, qd, checkin');
  const [pointsAliases, setPointsAliases] = useState('积分, jf, points');
  const [helpAliases, setHelpAliases] = useState('帮助, 菜单, help');
  const [selected, setSelected] = useState<EconomyAccount | null>(null);
  const [delta, setDelta] = useState(0);
  const [reason, setReason] = useState('管理员调整');
  const [legacyFile, setLegacyFile] = useState<File | null>(null);
  const [legacyPreview, setLegacyPreview] = useState<LegacyAstrBotPreview | null>(null);
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [deadLetterPlayerUIDs, setDeadLetterPlayerUIDs] = useState<Record<number, string>>({});

  const configQuery = useQuery({ queryKey: ['economy', 'config'], queryFn: economyApi.config });
  const summaryQuery = useQuery({ queryKey: ['economy', 'summary'], queryFn: economyApi.summary });
  const bridgeQuery = useQuery({ queryKey: ['economy', 'game-event-bridge'], queryFn: economyApi.bridgeStatus, refetchInterval: 10000 });
  const bridgeObservationsQuery = useQuery({ queryKey: ['economy', 'game-event-bridge', 'observations'], queryFn: economyApi.bridgeObservations, refetchInterval: 15000 });
  const bridgeDeadLettersQuery = useQuery({ queryKey: ['economy', 'game-event-bridge', 'dead-letters'], queryFn: () => economyApi.bridgeDeadLetters('pending'), refetchInterval: 15000 });
  const accountsQuery = useQuery({ queryKey: ['economy', 'accounts', search], queryFn: () => economyApi.accounts(search) });
  const ledgerQuery = useQuery({
    queryKey: ['economy', 'ledger', selected?.player_uid],
    queryFn: () => economyApi.ledger(selected?.player_uid || ''),
    enabled: Boolean(selected?.player_uid),
  });
  const checkinsQuery = useQuery({
    queryKey: ['economy', 'checkins', selected?.player_uid],
    queryFn: () => economyApi.checkins(selected?.player_uid || ''),
    enabled: Boolean(selected?.player_uid),
  });

  useEffect(() => {
    if (!configQuery.data) return;
    setPrefix(configQuery.data.command_prefix);
    setAllowBareCommands(configQuery.data.allow_bare_commands);
    setDailyPoints(configQuery.data.daily_checkin_points);
    setStreakEnabled(configQuery.data.checkin_streak_enabled);
    setStreakBonusPerDay(configQuery.data.checkin_streak_bonus_per_day);
    setStreakMaxDays(configQuery.data.checkin_streak_max_days);
    setCycleDays(configQuery.data.checkin_cycle_days);
    setCycleBonus(configQuery.data.checkin_cycle_bonus);
    setCheckinAliases(configQuery.data.checkin_aliases.join(', '));
    setPointsAliases(configQuery.data.points_aliases.join(', '));
    setHelpAliases(configQuery.data.help_aliases.join(', '));
  }, [configQuery.data]);

  const parseAliases = (value: string) => value.split(/[,，\n]/).map((item) => item.trim()).filter(Boolean);

  const saveConfig = useMutation({
    mutationFn: () => economyApi.updateConfig({
      command_prefix: prefix,
      allow_bare_commands: allowBareCommands,
      daily_checkin_points: dailyPoints,
      checkin_streak_enabled: streakEnabled,
      checkin_streak_bonus_per_day: streakBonusPerDay,
      checkin_streak_max_days: streakMaxDays,
      checkin_cycle_days: cycleDays,
      checkin_cycle_bonus: cycleBonus,
      checkin_aliases: parseAliases(checkinAliases),
      points_aliases: parseAliases(pointsAliases),
      help_aliases: parseAliases(helpAliases),
    }),
    onSuccess: async () => {
      setNotice({ type: 'success', text: prefix === '' ? '已启用无前缀命令模式。' : `命令前缀已更新为“${prefix}”。` });
      await queryClient.invalidateQueries({ queryKey: ['economy', 'config'] });
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const repairBridge = useMutation({
    mutationFn: economyApi.repairBridge,
    onSuccess: async (result) => {
      setNotice({ type: result.reload_required ? 'error' : 'success', text: result.reload_required ? `日志开关已写入，但热重载失败：${result.reload_error || '请重启服务端'}` : '已启用聊天、PlayerUID、捕捉、死亡、登录和制作日志，事件桥接将在数秒内生效。' });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge', 'observations'] }),
      ]);
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const replayDeadLetter = useMutation({
    mutationFn: ({ id, playerUID }: { id: number; playerUID: string }) => economyApi.replayBridgeDeadLetter(id, playerUID),
    onSuccess: async (result) => {
      setNotice({ type: 'success', text: `死信 #${result.dead_letter_id} 已重放；重复事件会由幂等账本自动跳过。` });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge', 'observations'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge', 'dead-letters'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'summary'] }),
      ]);
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const dismissDeadLetter = useMutation({
    mutationFn: (id: number) => economyApi.dismissBridgeDeadLetter(id),
    onSuccess: async (result) => {
      setNotice({ type: 'success', text: `死信 #${result.dead_letter_id} 已忽略。` });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'game-event-bridge', 'dead-letters'] }),
      ]);
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

  const inspectLegacy = useMutation({
    mutationFn: () => {
      if (!legacyFile) throw new Error('请先选择 AstrBot 插件的 palpanel.sqlite3。');
      return economyApi.inspectAstrBot(legacyFile);
    },
    onSuccess: (preview) => {
      setLegacyPreview(preview);
      setNotice({
        type: 'success',
        text: `检查完成：可导入 ${number.format(preview.importable_points)} 积分，涉及 ${preview.importable_accounts} 个账户。`,
      });
    },
    onError: (error) => {
      setLegacyPreview(null);
      setNotice({ type: 'error', text: getErrorMessage(error) });
    },
  });

  const importLegacy = useMutation({
    mutationFn: () => {
      if (!legacyFile || !legacyPreview) throw new Error('请先上传并检查旧积分数据库。');
      return economyApi.importAstrBot(legacyFile);
    },
    onSuccess: async (result) => {
      setNotice({
        type: 'success',
        text: `迁移完成：导入 ${number.format(result.imported_points)} 积分、${result.imported_accounts} 个账户、${result.imported_checkins} 条签到记录。`,
      });
      setLegacyPreview(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['economy', 'summary'] }),
        queryClient.invalidateQueries({ queryKey: ['economy', 'accounts'] }),
      ]);
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const summary = summaryQuery.data;
  const accounts = accountsQuery.data?.items || [];
  const loading = configQuery.isLoading || summaryQuery.isLoading || accountsQuery.isLoading;
  const commandExamples = useMemo(() => [
    parseAliases(checkinAliases)[0] || '签到',
    parseAliases(pointsAliases)[0] || '积分',
    parseAliases(helpAliases)[0] || '帮助',
  ].flatMap((command) => {
    const values = [`${prefix}${command}`];
    if (prefix && allowBareCommands) values.push(command);
    return values;
  }), [allowBareCommands, checkinAliases, helpAliases, pointsAliases, prefix]);
  const streakPreview = useMemo(() => Array.from({ length: Math.min(Math.max(streakMaxDays, 1), 14) }, (_, index) => {
    const day = index + 1;
    const streakBonus = streakEnabled ? Math.min(day - 1, Math.max(streakMaxDays - 1, 0)) * streakBonusPerDay : 0;
    const dayCycleBonus = streakEnabled && cycleDays > 0 && day % cycleDays === 0 ? cycleBonus : 0;
    return { day, points: dailyPoints + streakBonus + dayCycleBonus };
  }), [cycleBonus, cycleDays, dailyPoints, streakBonusPerDay, streakEnabled, streakMaxDays]);

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
            <label className="flex items-start gap-3 rounded-xl border border-sky-100 bg-sky-50 p-3 text-sm text-slate-700">
              <input type="checkbox" className="mt-0.5 size-4" checked={allowBareCommands} onChange={(event) => setAllowBareCommands(event.target.checked)} />
              <span><strong className="block">同时允许无前缀命令</strong><span className="mt-1 block text-xs text-slate-500">启用后，配置了 <code className="font-mono">!</code> 前缀时，玩家发送“签到”或“!签到”都能触发。</span></span>
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs font-bold text-slate-500">每日签到积分</span>
              <input type="number" min={0} max={1000000} value={dailyPoints} onChange={(event) => setDailyPoints(Number(event.target.value))} className="pp-input w-full" />
            </label>
            <label className="flex items-start gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-700">
              <input type="checkbox" className="mt-0.5 size-4" checked={streakEnabled} onChange={(event) => setStreakEnabled(event.target.checked)} />
              <span><strong className="block">启用连续签到奖励</strong><span className="mt-1 block text-xs text-slate-500">漏签后从第1天重新计算；同一天重复发送不会重复加分。</span></span>
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">每日递增奖励</span><input type="number" min={0} max={1000000} value={streakBonusPerDay} onChange={(event) => setStreakBonusPerDay(Number(event.target.value))} className="pp-input w-full" disabled={!streakEnabled} /></label>
              <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">递增封顶天数</span><input type="number" min={1} max={365} value={streakMaxDays} onChange={(event) => setStreakMaxDays(Number(event.target.value))} className="pp-input w-full" disabled={!streakEnabled} /></label>
              <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">周期奖励间隔</span><input type="number" min={0} max={365} value={cycleDays} onChange={(event) => setCycleDays(Number(event.target.value))} className="pp-input w-full" disabled={!streakEnabled} /><span className="mt-1 block text-[11px] text-slate-400">填0关闭周期奖励。</span></label>
              <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">周期额外奖励</span><input type="number" min={0} max={1000000} value={cycleBonus} onChange={(event) => setCycleBonus(Number(event.target.value))} className="pp-input w-full" disabled={!streakEnabled || cycleDays === 0} /></label>
            </div>
            <div className="rounded-xl border border-sky-100 bg-sky-50 p-3">
              <div className="mb-2 text-xs font-bold text-sky-700">连续签到积分预览</div>
              <div className="flex flex-wrap gap-2">{streakPreview.map((item) => <span key={item.day} className="rounded-lg bg-white px-2 py-1 text-xs font-bold text-slate-700 shadow-sm">第{item.day}天 {number.format(item.points)}</span>)}</div>
            </div>
            <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">签到命令别名</span><input value={checkinAliases} onChange={(event) => setCheckinAliases(event.target.value)} className="pp-input w-full" placeholder="签到, qd, checkin" /><span className="mt-1 block text-[11px] text-slate-400">使用逗号分隔，游戏中任一别名都可触发。</span></label>
            <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">积分查询别名</span><input value={pointsAliases} onChange={(event) => setPointsAliases(event.target.value)} className="pp-input w-full" placeholder="积分, jf, points" /></label>
            <label className="block"><span className="mb-1.5 block text-xs font-bold text-slate-500">帮助命令别名</span><input value={helpAliases} onChange={(event) => setHelpAliases(event.target.value)} className="pp-input w-full" placeholder="帮助, 菜单, help" /></label>
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

      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="mb-2 flex items-center gap-2"><Activity size={18} className="text-emerald-500" /><h2 className="font-black text-slate-900">游戏事件桥接</h2></div>
            <p className="max-w-3xl text-sm leading-6 text-slate-500">直接读取 PalDefender 日志，将聊天命令、捕捉、击杀、登录和制作事件送入积分与任务系统。无需额外部署外部转发器。</p>
          </div>
          <button type="button" className="pp-btn pp-btn--primary shrink-0" disabled={repairBridge.isPending} onClick={() => repairBridge.mutate()}>{repairBridge.isPending ? <LoaderCircle className="animate-spin" size={15} /> : <Settings2 size={15} />}一键修复日志开关</button>
        </div>
        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-9">
          <BridgeMetric label="桥接进程" ok={Boolean(bridgeQuery.data?.bridge.running)} text={bridgeQuery.data?.bridge.running ? '运行中' : '未运行'} />
          <BridgeMetric label="已处理事件" ok={(bridgeQuery.data?.bridge.processed_events || 0) > 0} text={number.format(bridgeQuery.data?.bridge.processed_events || 0)} />
          <BridgeMetric label="待处理死信" ok={(bridgeQuery.data?.bridge.pending_dead_letters || 0) === 0} text={number.format(bridgeQuery.data?.bridge.pending_dead_letters || 0)} />
          <BridgeMetric label="玩家未匹配" ok={(bridgeQuery.data?.bridge.unmatched_players || 0) === 0} text={number.format(bridgeQuery.data?.bridge.unmatched_players || 0)} />
          <BridgeMetric label="游标文件" ok={(bridgeQuery.data?.bridge.cursor_files || 0) > 0} text={number.format(bridgeQuery.data?.bridge.cursor_files || 0)} />
          <BridgeMetric label="轮转恢复" ok={(bridgeQuery.data?.bridge.rotation_resets || 0) === 0} text={number.format(bridgeQuery.data?.bridge.rotation_resets || 0)} />
          <BridgeMetric label="当前在线" ok={!bridgeQuery.data?.bridge.last_online_error} text={number.format(bridgeQuery.data?.bridge.online_players || 0)} />
          <BridgeMetric label="在线跟踪玩家" ok={!bridgeQuery.data?.bridge.last_online_error} text={number.format(bridgeQuery.data?.bridge.tracked_online_players || 0)} />
          <BridgeMetric label="已结算在线分钟" ok={!bridgeQuery.data?.bridge.last_online_error} text={number.format(bridgeQuery.data?.bridge.online_minutes_emitted || 0)} />
          <BridgeMetric label="日志扫描耗时" ok={(bridgeQuery.data?.bridge.last_scan_duration_ms || 0) < 1000} text={`${number.format(bridgeQuery.data?.bridge.last_scan_duration_ms || 0)} ms`} />
          <BridgeMetric label="玩家快照年龄" ok={(bridgeQuery.data?.bridge.player_snapshot_age_seconds || 0) <= 45} text={`${number.format(bridgeQuery.data?.bridge.player_snapshot_age_seconds || 0)} 秒`} />
        </div>
        <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {Object.entries(bridgeQuery.data?.bridge.configuration || {}).map(([key, enabled]) => <div key={key} className={`flex items-center justify-between rounded-xl border px-3 py-2 text-xs font-bold ${enabled ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-amber-200 bg-amber-50 text-amber-800'}`}><span>{key}</span><span>{enabled ? '已启用' : '未启用'}</span></div>)}
        </div>
        <div className="mt-4 grid gap-3 lg:grid-cols-3">
          <div className="rounded-xl border border-sky-100 bg-sky-50 p-3 text-xs leading-5 text-sky-800"><strong className="block">捕捉任务</strong><code className="font-mono">PAL_CAPTURED</code>，数量字段填 <code className="font-mono">count</code>。限定某种帕鲁时，过滤条件使用内部 ID，例如 <code className="font-mono">{`{"pal_id":"SheepBall"}`}</code>。</div>
          <div className="rounded-xl border border-emerald-100 bg-emerald-50 p-3 text-xs leading-5 text-emerald-800"><strong className="block">签到任务</strong>玩家当天首次签到成功后会额外生成 <code className="font-mono">CHECKIN_COMPLETED</code>，数量字段填 <code className="font-mono">count</code>；重复签到不会推进任务。</div>
          <div className="rounded-xl border border-amber-100 bg-amber-50 p-3 text-xs leading-5 text-amber-800"><strong className="block">击杀任务</strong><code className="font-mono">PAL_KILLED</code> 依赖 PalDefender 死亡日志。不同版本日志可能只提供目标名称；先查看下方日志样本，再选择 <code className="font-mono">target_name</code> 或 <code className="font-mono">pal_id</code> 过滤。</div>
          <div className="rounded-xl border border-violet-100 bg-violet-50 p-3 text-xs leading-5 text-violet-800"><strong className="block">在线时长任务</strong>事件类型选择 <code className="font-mono">PLAYER_ONLINE</code>，数量字段填写 <code className="font-mono">minutes</code>。系统复用实时监控的玩家快照，最多每30秒结算一次；不会再单独轮询 PalDefender 玩家接口。</div>
        </div>
        {bridgeQuery.data?.bridge.last_error && <div className="mt-4 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">{bridgeQuery.data.bridge.last_error}</div>}
        {bridgeQuery.data?.bridge.last_online_error && <div className="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">在线时长采样失败：{bridgeQuery.data.bridge.last_online_error}</div>}
        {(bridgeQuery.data?.online_tracking || []).length > 0 && <details className="mt-4 rounded-xl border border-violet-100 bg-violet-50/50 p-3 text-xs text-slate-600"><summary className="cursor-pointer font-bold text-violet-800">在线时长跟踪明细（{bridgeQuery.data?.online_tracking.length}）</summary><div className="mt-3 grid gap-2 lg:grid-cols-2">{(bridgeQuery.data?.online_tracking || []).map((item) => <div key={item.player_uid} className="rounded-lg border border-violet-100 bg-white p-2"><div className="flex items-center justify-between gap-2"><strong className="truncate text-slate-800">{item.nickname || item.player_uid}</strong><span className={`rounded-full px-2 py-0.5 font-bold ${item.active ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{item.active ? '在线' : '离线'}</span></div><div className="mt-1 font-mono text-[11px] text-slate-400">{item.player_uid}</div><div className="mt-1 text-[11px]">已提交 {number.format(item.total_emitted_minutes)} 分钟 · 待累计 {number.format(item.pending_seconds)} 秒</div></div>)}</div></details>}
        <div className="mt-4 rounded-xl border border-slate-200 bg-slate-50/70 p-4">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h3 className="flex items-center gap-2 text-sm font-black text-slate-800"><CircleAlert size={16} className="text-amber-500" />事件死信</h3>
              <p className="mt-1 text-xs leading-5 text-slate-500">解析失败、玩家未匹配或任务处理失败的日志会保存在数据库中。修正配置后可安全重放；原事件ID保持不变，不会重复加分或发货。</p>
            </div>
            <span className="rounded-full bg-amber-100 px-3 py-1 text-xs font-black text-amber-800">待处理 {number.format(bridgeDeadLettersQuery.data?.pending || 0)}</span>
          </div>
          <div className="mt-3 space-y-3">
            {(bridgeDeadLettersQuery.data?.items || []).map((item) => (
              <div key={item.id} className="rounded-xl border border-slate-200 bg-white p-3 shadow-sm">
                <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="rounded-md bg-rose-50 px-2 py-1 font-mono text-[11px] font-bold text-rose-700">#{item.id} {item.event_type || 'UNKNOWN'}</span>
                      <span className="text-xs font-bold text-slate-700">{item.nickname || item.player_hint || item.player_uid || '玩家未识别'}</span>
                      <span className="text-[11px] text-slate-400">尝试 {item.attempts} 次 · {new Date(item.created_at).toLocaleString()}</span>
                    </div>
                    <p className="mt-2 text-xs font-semibold leading-5 text-rose-600">{item.last_error || item.reason}</p>
                    <p className="mt-2 break-all rounded-lg bg-slate-950 px-3 py-2 font-mono text-[11px] leading-5 text-slate-300">{item.sample || item.raw_line || '-'}</p>
                    <details className="mt-2 text-[11px] text-slate-500"><summary className="cursor-pointer font-bold">查看Payload与事件ID</summary><pre className="mt-2 overflow-auto rounded-lg bg-slate-100 p-2 font-mono">{JSON.stringify({ event_id: item.event_id, payload: item.payload }, null, 2)}</pre></details>
                  </div>
                  <div className="w-full shrink-0 space-y-2 xl:w-80">
                    <input
                      value={deadLetterPlayerUIDs[item.id] || ''}
                      onChange={(event) => setDeadLetterPlayerUIDs((current) => ({ ...current, [item.id]: event.target.value }))}
                      className="pp-input w-full font-mono text-xs"
                      placeholder="可选：人工指定 PlayerUID"
                    />
                    <div className="grid grid-cols-2 gap-2">
                      <button type="button" className="pp-btn pp-btn--primary justify-center" disabled={replayDeadLetter.isPending || dismissDeadLetter.isPending} onClick={() => replayDeadLetter.mutate({ id: item.id, playerUID: deadLetterPlayerUIDs[item.id] || '' })}>{replayDeadLetter.isPending ? <LoaderCircle size={14} className="animate-spin" /> : <RefreshCw size={14} />}重放</button>
                      <button type="button" className="pp-btn justify-center" disabled={replayDeadLetter.isPending || dismissDeadLetter.isPending} onClick={() => { if (window.confirm(`确认忽略死信 #${item.id}？`)) dismissDeadLetter.mutate(item.id); }}><AlertTriangle size={14} />忽略</button>
                    </div>
                  </div>
                </div>
              </div>
            ))}
            {!bridgeDeadLettersQuery.isLoading && (bridgeDeadLettersQuery.data?.items.length || 0) === 0 && <div className="rounded-xl border border-emerald-100 bg-emerald-50 px-4 py-5 text-center text-sm font-bold text-emerald-700"><CheckCircle2 className="mx-auto mb-2" size={20} />当前没有待处理死信。</div>}
          </div>
        </div>
        {(bridgeQuery.data?.offsets || []).length > 0 && <details className="mt-4 rounded-xl border border-slate-200 bg-white p-3 text-xs text-slate-500"><summary className="cursor-pointer font-bold text-slate-700">持久化日志游标（{bridgeQuery.data?.offsets.length}）</summary><div className="mt-3 space-y-2">{(bridgeQuery.data?.offsets || []).map((item) => <div key={item.path} className="grid gap-1 rounded-lg bg-slate-50 p-2 lg:grid-cols-[minmax(0,1fr)_auto_auto]"><span className="truncate font-mono" title={item.path}>{item.path}</span><span>偏移 {number.format(item.offset)} / {number.format(item.file_size)}</span><span>{item.reset_count > 0 ? `恢复 ${item.reset_count} 次 · ${item.last_reset_reason || '-'}` : '未发生轮转重置'}</span></div>)}</div></details>}
        <div className="mt-4 overflow-x-auto rounded-xl border border-slate-100">
          <table className="min-w-full text-left text-xs"><thead className="bg-slate-50 font-bold text-slate-500"><tr><th className="px-3 py-2">时间</th><th className="px-3 py-2">事件</th><th className="px-3 py-2">玩家</th><th className="px-3 py-2">状态</th><th className="px-3 py-2">日志样本</th></tr></thead><tbody className="divide-y divide-slate-100">{(bridgeObservationsQuery.data?.items || []).map((item) => <tr key={item.id}><td className="whitespace-nowrap px-3 py-2 text-slate-400">{new Date(item.created_at).toLocaleString()}</td><td className="px-3 py-2 font-mono font-bold text-slate-700">{item.event_type || '-'}</td><td className="px-3 py-2 text-slate-600">{item.nickname || item.player_uid || '-'}</td><td className="px-3 py-2"><span className={`rounded-full px-2 py-1 font-bold ${item.status === 'processed' || item.status === 'replayed' ? 'bg-emerald-50 text-emerald-700' : item.status === 'unmatched_player' || item.status === 'cursor_reset' ? 'bg-amber-50 text-amber-700' : 'bg-rose-50 text-rose-700'}`}>{item.status === 'processed' ? '已处理' : item.status === 'replayed' ? '已重放' : item.status === 'unmatched_player' ? '玩家未匹配' : item.status === 'parse_failed' ? '解析失败' : item.status === 'cursor_reset' ? '游标恢复' : '失败'}</span>{item.reason && <div className="mt-1 max-w-72 text-[11px] text-rose-500">{item.reason}</div>}</td><td className="max-w-xl truncate px-3 py-2 font-mono text-[11px] text-slate-400" title={item.sample}>{item.sample || '-'}</td></tr>)}{!bridgeObservationsQuery.isLoading && (bridgeObservationsQuery.data?.items.length || 0) === 0 && <tr><td colSpan={5} className="px-3 py-8 text-center text-slate-400">等待新的游戏聊天、捕捉或击杀日志。更新后首次启动只从日志末尾开始，不会回放旧事件。</td></tr>}</tbody></table>
        </div>
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-5 xl:flex-row xl:items-start xl:justify-between">
          <div className="max-w-2xl">
            <div className="mb-2 flex items-center gap-2"><Database size={18} className="text-indigo-500" /><h2 className="font-black text-slate-900">AstrBot旧积分迁移</h2></div>
            <p className="text-sm leading-6 text-slate-500">上传 AstrBot 插件目录中的 <code className="rounded bg-slate-100 px-1.5 py-0.5">palpanel.sqlite3</code>。系统按QQ绑定的PlayerUID迁移余额和签到记录，并通过差额导入防止重复加分。</p>
            <div className="mt-3 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-800"><AlertTriangle className="mt-0.5 shrink-0" size={15} />迁移前应停止旧插件的签到和积分写入。旧库余额下降时不会自动扣除面板积分，只会在检查结果中标记。</div>
          </div>
          <div className="w-full max-w-xl space-y-3">
            <label className="block rounded-xl border border-dashed border-slate-300 bg-slate-50 p-4">
              <span className="mb-2 flex items-center gap-2 text-xs font-bold text-slate-600"><Upload size={15} />选择SQLite数据库</span>
              <input
                type="file"
                accept=".db,.sqlite,.sqlite3,application/x-sqlite3"
                onChange={(event) => {
                  setLegacyFile(event.target.files?.[0] || null);
                  setLegacyPreview(null);
                }}
                className="block w-full text-xs text-slate-500 file:mr-3 file:rounded-lg file:border-0 file:bg-white file:px-3 file:py-2 file:text-xs file:font-bold file:text-slate-700 file:shadow-sm"
              />
              {legacyFile && <span className="mt-2 block truncate text-xs text-slate-400">{legacyFile.name} · {number.format(legacyFile.size)} bytes</span>}
            </label>
            <div className="grid gap-2 sm:grid-cols-2">
              <button type="button" disabled={!legacyFile || inspectLegacy.isPending || importLegacy.isPending} onClick={() => inspectLegacy.mutate()} className="pp-button justify-center"><FileSearch size={15} />{inspectLegacy.isPending ? '正在检查…' : '检查数据库'}</button>
              <button type="button" disabled={!legacyPreview || importLegacy.isPending || inspectLegacy.isPending} onClick={() => importLegacy.mutate()} className="pp-btn pp-btn--primary justify-center"><Upload size={15} />{importLegacy.isPending ? '正在迁移…' : '确认迁移'}</button>
            </div>
          </div>
        </div>

        {legacyPreview && (
          <div className="mt-5 grid gap-3 border-t border-slate-100 pt-5 sm:grid-cols-2 lg:grid-cols-4">
            <MigrationMetric label="旧库总积分" value={legacyPreview.source_points} />
            <MigrationMetric label="本次可导入" value={legacyPreview.importable_points} emphasis />
            <MigrationMetric label="可导入账户" value={legacyPreview.importable_accounts} suffix="个" />
            <MigrationMetric label="签到记录" value={legacyPreview.checkins} suffix="条" />
            <MigrationMetric label="未绑定账户" value={legacyPreview.unbound_accounts} suffix="个" warning={legacyPreview.unbound_accounts > 0} />
            <MigrationMetric label="余额下降" value={legacyPreview.decreased_balance} suffix="个" warning={legacyPreview.decreased_balance > 0} />
            <MigrationMetric label="已是最新" value={legacyPreview.already_current} suffix="个" />
            <MigrationMetric label="零余额" value={legacyPreview.zero_balance} suffix="个" />
          </div>
        )}
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
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm xl:col-span-2">
          <h2 className="mb-4 font-black text-slate-900">签到记录</h2>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{(checkinsQuery.data?.items || []).map((item) => <div key={item.local_date} className="rounded-xl border border-slate-100 p-3"><div className="flex items-center justify-between"><span className="text-sm font-bold text-slate-700">{item.local_date}</span><span className="text-sm font-black text-emerald-600">+{number.format(item.points)}</span></div><div className="mt-2 text-[11px] leading-5 text-slate-400">连续第 {item.streak_day} 天 · 基础 {item.base_points} · 连续 {item.streak_bonus} · 周期 {item.cycle_bonus}</div></div>)}{!checkinsQuery.isLoading && (checkinsQuery.data?.items.length || 0) === 0 && <div className="py-8 text-sm text-slate-400">暂无签到记录。</div>}</div>
        </div>
      </section>}
    </div>
  );
};

const Metric: React.FC<{ label: string; value: number; suffix?: string; icon: React.ReactNode }> = ({ label, value, suffix, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm"><div className="mb-4 flex items-center justify-between text-slate-400"><span className="text-xs font-bold uppercase tracking-wider">{label}</span><span className="rounded-lg bg-sky-50 p-2 text-sky-500">{icon}</span></div><div className="text-2xl font-black tracking-tight text-slate-900">{number.format(value)}{suffix && <span className="ml-1 text-sm text-slate-400">{suffix}</span>}</div></div>
);

const BridgeMetric: React.FC<{ label: string; ok: boolean; text: string }> = ({ label, ok, text }) => (
  <div className={`rounded-xl border p-3 ${ok ? 'border-emerald-100 bg-emerald-50' : 'border-amber-200 bg-amber-50'}`}>
    <div className="flex items-center gap-2 text-xs font-bold text-slate-500">{ok ? <CheckCircle2 size={14} className="text-emerald-600" /> : <CircleAlert size={14} className="text-amber-600" />}{label}</div>
    <div className={`mt-2 text-lg font-black ${ok ? 'text-emerald-700' : 'text-amber-700'}`}>{text}</div>
  </div>
);

const MigrationMetric: React.FC<{ label: string; value: number; suffix?: string; emphasis?: boolean; warning?: boolean }> = ({ label, value, suffix, emphasis, warning }) => (
  <div className={`rounded-xl border p-3 ${warning ? 'border-amber-200 bg-amber-50' : emphasis ? 'border-emerald-200 bg-emerald-50' : 'border-slate-100 bg-slate-50'}`}>
    <div className="text-[11px] font-bold text-slate-500">{label}</div>
    <div className={`mt-1 text-lg font-black ${warning ? 'text-amber-700' : emphasis ? 'text-emerald-700' : 'text-slate-800'}`}>{number.format(value)}{suffix && <span className="ml-1 text-xs font-bold">{suffix}</span>}</div>
  </div>
);
