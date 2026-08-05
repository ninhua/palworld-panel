import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  CheckCircle2,
  CircleAlert,
  ClipboardCheck,
  Crosshair,
  LoaderCircle,
  MapPin,
  RefreshCw,
  Save,
  Search,
  ShieldCheck,
  UserMinus,
  UserPlus,
  Users,
} from 'lucide-react';
import { bossApi, type BossSummon } from '../api/boss';
import {
  bossRegistrationApi,
  type BossParticipant,
  type BossRegistrationPolicyInput,
} from '../api/bossRegistration';
import { getErrorMessage } from '../api/client';

interface Notice {
  type: 'success' | 'error';
  text: string;
}

interface ParticipantDraft {
  playerUID: string;
  nickname: string;
  steamID: string;
}

const emptyParticipantDraft = (): ParticipantDraft => ({ playerUID: '', nickname: '', steamID: '' });
const number = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 });
const asRecord = (value: unknown): Record<string, unknown> =>
  value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
const metadataString = (value: unknown, key: string) => {
  const item = asRecord(value)[key];
  return typeof item === 'string' ? item.trim() : '';
};
const isFixedBoss = (summon: BossSummon) => metadataString(summon.metadata, 'activity_kind') === 'fixed_boss';
const formatTime = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—';
const shortID = (value: string) => value.length <= 8 ? value : value.slice(-8);

const participantStatusLabel: Record<BossParticipant['status'], string> = {
  registered: '已报名',
  checked_in: '已到场',
  cancelled: '已取消',
};

const areaStatusLabel: Record<BossParticipant['area_status'], string> = {
  unknown: '待核验',
  eligible: '区域内',
  outside: '区域外',
  disabled: '无需核验',
};

const participantStatusClass: Record<BossParticipant['status'], string> = {
  registered: 'bg-amber-50 text-amber-700',
  checked_in: 'bg-emerald-50 text-emerald-700',
  cancelled: 'bg-slate-100 text-slate-600',
};

const eventLabel: Record<string, string> = {
  registered: '报名',
  area_checked: '区域核验',
  cancelled: '取消报名',
  policy_rechecked: '策略重算',
};

const FieldLabel: React.FC<React.PropsWithChildren> = ({ children }) => (
  <span className="mb-1.5 block text-xs font-bold text-slate-500">{children}</span>
);

const Metric: React.FC<{ label: string; value: React.ReactNode; detail: string; icon: React.ReactNode }> = ({ label, value, detail, icon }) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <span className="text-[11px] font-black uppercase tracking-[0.16em] text-slate-400">{label}</span>
      <span className="text-rose-500">{icon}</span>
    </div>
    <strong className="mt-3 block truncate text-xl font-black text-slate-900">{value}</strong>
    <span className="mt-1 block text-[11px] font-semibold text-slate-500">{detail}</span>
  </div>
);

export const BossRegistration: React.FC = () => {
  const queryClient = useQueryClient();
  const [notice, setNotice] = useState<Notice | null>(null);
  const [selectedSummonID, setSelectedSummonID] = useState('');
  const [search, setSearch] = useState('');
  const [includeCancelled, setIncludeCancelled] = useState(false);
  const [participantDraft, setParticipantDraft] = useState<ParticipantDraft>(emptyParticipantDraft);
  const [policyDraft, setPolicyDraft] = useState<Required<BossRegistrationPolicyInput>>({
    enabled: false,
    max_players: 0,
    radius: 0,
    use_z: false,
    allow_active: false,
  });

  const summonsQuery = useQuery({
    queryKey: ['boss-registration', 'summons'],
    queryFn: () => bossApi.summons(''),
    refetchInterval: 10_000,
  });

  const summons = useMemo(() => {
    const query = search.trim().toLowerCase();
    return (summonsQuery.data?.items || [])
      .filter(isFixedBoss)
      .filter((item) => item.status === 'pending' || item.status === 'active')
      .filter((item) => !query || `${item.template_name} ${item.id} ${item.location.label || ''}`.toLowerCase().includes(query))
      .sort((left, right) => {
        if (left.status !== right.status) return left.status === 'active' ? -1 : 1;
        return right.requested_at.localeCompare(left.requested_at);
      });
  }, [summonsQuery.data, search]);

  useEffect(() => {
    if (selectedSummonID && summons.some((item) => item.id === selectedSummonID)) return;
    setSelectedSummonID(summons[0]?.id || '');
  }, [summons, selectedSummonID]);

  const selectedSummon = useMemo(
    () => summons.find((item) => item.id === selectedSummonID),
    [summons, selectedSummonID],
  );

  const snapshotQuery = useQuery({
    queryKey: ['boss-registration', 'snapshot', selectedSummonID, includeCancelled],
    queryFn: () => bossRegistrationApi.snapshot(selectedSummonID, includeCancelled),
    enabled: Boolean(selectedSummonID),
    refetchInterval: 10_000,
  });

  const snapshot = snapshotQuery.data;
  const policy = snapshot?.policy;
  const participants = snapshot?.participants || [];
  const events = snapshot?.events || [];

  useEffect(() => {
    if (!policy || policy.summon_id !== selectedSummonID) return;
    setPolicyDraft({
      enabled: policy.enabled,
      max_players: policy.max_players,
      radius: policy.radius,
      use_z: policy.use_z,
      allow_active: policy.allow_active,
    });
  }, [policy, selectedSummonID]);

  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: ['boss-registration'] });
  };

  const configureMutation = useMutation({
    mutationFn: () => bossRegistrationApi.configure(selectedSummonID, policyDraft),
    onSuccess: async (value) => {
      setNotice({ type: 'success', text: value.policy.enabled ? '报名策略已保存并重新核验现有参与者。' : '报名已关闭。' });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const registerMutation = useMutation({
    mutationFn: () => bossRegistrationApi.registerOnline(selectedSummonID, {
      player_uid: participantDraft.playerUID.trim(),
      nickname: participantDraft.nickname.trim(),
      steam_id: participantDraft.steamID.trim(),
      metadata: { registration_source: 'panel' },
    }),
    onSuccess: async (value) => {
      const location = value.participant.area_status === 'disabled'
        ? '，该场次无需区域核验'
        : value.location_lookup?.found
          ? '并已读取当前位置'
          : '，但当前位置暂不可用';
      setNotice({ type: 'success', text: `${value.duplicate ? '已有报名记录' : '报名已创建'}${location}。` });
      setParticipantDraft(emptyParticipantDraft());
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const checkMutation = useMutation({
    mutationFn: (participant: BossParticipant) => bossRegistrationApi.checkOnline(selectedSummonID, {
      player_uid: participant.player_uid,
      nickname: participant.nickname,
      steam_id: participant.steam_id,
      metadata: { registration_source: 'panel' },
    }),
    onSuccess: async (value) => {
      setNotice({
        type: value.participant.eligible ? 'success' : 'error',
        text: value.participant.eligible
          ? `${value.participant.nickname || value.participant.player_uid} 已通过区域核验。`
          : `${value.participant.nickname || value.participant.player_uid} 当前不在活动区域。`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const cancelMutation = useMutation({
    mutationFn: (participant: BossParticipant) => bossRegistrationApi.cancel(selectedSummonID, participant.player_uid, 'cancelled by panel administrator'),
    onSuccess: async (value) => {
      setNotice({ type: 'success', text: `${value.participant.nickname || value.participant.player_uid} 的报名已取消，名额已释放。` });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const refreshOnlineMutation = useMutation({
    mutationFn: () => bossRegistrationApi.refreshOnline(selectedSummonID),
    onSuccess: async (value) => {
      setNotice({
        type: value.failed > 0 ? 'error' : 'success',
        text: `在线位置刷新完成：更新 ${value.updated}，未取得位置 ${value.unavailable}，失败 ${value.failed}。`,
      });
      await refresh();
    },
    onError: (error) => setNotice({ type: 'error', text: getErrorMessage(error) }),
  });

  const availableText = !policy
    ? '—'
    : policy.max_players <= 0
      ? '不限'
      : number.format(policy.available);

  return (
    <div className="mx-auto flex w-full max-w-[1550px] flex-col gap-5 p-4 sm:p-6 lg:p-8">
      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-bold uppercase tracking-[0.18em] text-rose-600"><ClipboardCheck size={15} />Boss 活动</div>
            <h1 className="text-2xl font-black tracking-tight text-slate-900">Boss 报名管理</h1>
            <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-500">配置固定坐标 Boss 场次的报名人数和区域资格。玩家可在游戏聊天中报名；位置核验通过 PalPanelBridge 0.1.24 的 CachedPlayerLocation 完成。</p>
          </div>
          <button type="button" onClick={() => void refresh()} className="pp-button"><RefreshCw size={14} />刷新</button>
        </div>
        {notice && <div className={`mt-4 rounded-xl border px-4 py-3 text-sm font-semibold ${notice.type === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>{notice.text}</div>}
      </section>

      <section className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="开放状态" value={policy?.open ? '开放' : '关闭'} detail={policy?.enabled ? '策略已启用' : '尚未启用'} icon={<ShieldCheck size={18} />} />
        <Metric label="已报名" value={policy?.registered || 0} detail={`剩余名额 ${availableText}`} icon={<Users size={18} />} />
        <Metric label="已到场" value={policy?.checked_in || 0} detail="通过区域核验" icon={<CheckCircle2 size={18} />} />
        <Metric label="区域外" value={policy?.outside || 0} detail={policy?.area_required ? `半径 ${number.format(policy.radius)}` : '未启用区域限制'} icon={<MapPin size={18} />} />
        <Metric label="活动中心" value={selectedSummon?.location.label || '固定坐标'} detail={selectedSummon ? `${number.format(selectedSummon.location.x)}, ${number.format(selectedSummon.location.y)}, ${number.format(selectedSummon.location.z)}` : '未选择场次'} icon={<Crosshair size={18} />} />
      </section>

      <section className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
        <div className="space-y-5">
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <h2 className="font-black text-slate-900">选择 Boss 场次</h2>
            <label className="relative mt-3 block"><Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={14} /><input value={search} onChange={(event) => setSearch(event.target.value)} className="pp-input w-full pl-9" placeholder="名称、场次ID或地点" /></label>
            <div className="mt-3 max-h-80 space-y-2 overflow-y-auto">
              {summons.map((summon) => <button key={summon.id} type="button" onClick={() => setSelectedSummonID(summon.id)} className={`w-full rounded-xl border p-3 text-left transition ${selectedSummonID === summon.id ? 'border-rose-300 bg-rose-50/50' : 'border-slate-200 hover:bg-slate-50'}`}>
                <div className="flex items-center justify-between gap-2"><strong className="truncate text-sm text-slate-800">{summon.template_name}</strong><span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${summon.status === 'active' ? 'bg-sky-50 text-sky-700' : 'bg-amber-50 text-amber-700'}`}>{summon.status === 'active' ? '进行中' : '等待执行'}</span></div>
                <div className="mt-1 font-mono text-[10px] text-slate-400">{shortID(summon.id)}</div>
                <div className="mt-1 truncate text-[11px] text-slate-500">{summon.location.label || `${summon.location.x}, ${summon.location.y}, ${summon.location.z}`}</div>
              </button>)}
              {!summonsQuery.isLoading && summons.length === 0 && <div className="rounded-xl bg-slate-50 py-8 text-center text-xs text-slate-400">暂无等待或进行中的固定 Boss 场次。</div>}
            </div>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <h2 className="font-black text-slate-900">游戏内命令</h2>
            <div className="mt-3 space-y-2 rounded-xl bg-slate-950 p-4 font-mono text-xs text-slate-200">
              <div>Boss列表</div>
              <div>Boss报名 [场次]</div>
              <div>Boss签到 [场次]</div>
              <div>Boss报名状态</div>
              <div>Boss取消 [场次]</div>
            </div>
            <p className="mt-3 text-xs leading-5 text-slate-500">配置了游戏命令前缀且关闭无前缀命令时，需要在上述命令前加相同前缀。</p>
          </div>
        </div>

        <div className="space-y-5">
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-black text-slate-900">报名策略</h2><p className="mt-1 text-xs text-slate-400">策略保存在本次召唤快照中。修改半径后会重新计算已有参与者状态。</p></div><button type="button" disabled={!selectedSummonID || configureMutation.isPending} onClick={() => configureMutation.mutate()} className="pp-btn pp-btn--primary">{configureMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <Save size={14} />}保存策略</button></div>
            <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
              <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={policyDraft.enabled} onChange={(event) => setPolicyDraft((current) => ({ ...current, enabled: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span className="text-sm font-black text-slate-800">启用报名</span></label>
              <label><FieldLabel>人数上限</FieldLabel><input type="number" min={0} max={1000} value={policyDraft.max_players} onChange={(event) => setPolicyDraft((current) => ({ ...current, max_players: Math.max(0, Math.trunc(Number(event.target.value) || 0)) }))} className="pp-input w-full" /><span className="mt-1 block text-[10px] text-slate-400">0 表示不限人数</span></label>
              <label><FieldLabel>区域半径</FieldLabel><input type="number" min={0} max={1000000} value={policyDraft.radius} onChange={(event) => setPolicyDraft((current) => ({ ...current, radius: Math.max(0, Number(event.target.value) || 0) }))} className="pp-input w-full" /><span className="mt-1 block text-[10px] text-slate-400">0 表示不核验区域</span></label>
              <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={policyDraft.use_z} onChange={(event) => setPolicyDraft((current) => ({ ...current, use_z: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span><span className="block text-sm font-black text-slate-800">计算高度</span><span className="text-[10px] text-slate-400">包含 Z 轴距离</span></span></label>
              <label className="flex items-center gap-3 rounded-xl border border-slate-200 p-3"><input type="checkbox" checked={policyDraft.allow_active} onChange={(event) => setPolicyDraft((current) => ({ ...current, allow_active: event.target.checked }))} className="h-4 w-4 rounded border-slate-300" /><span><span className="block text-sm font-black text-slate-800">开始后可报名</span><span className="text-[10px] text-slate-400">活动进行中仍开放</span></span></label>
            </div>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-black text-slate-900">参与者</h2><p className="mt-1 text-xs text-slate-400">批量刷新只更新当前在线且 Bridge 能读取到位置的玩家。</p></div><div className="flex flex-wrap gap-2"><label className="flex items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-xs font-bold text-slate-500"><input type="checkbox" checked={includeCancelled} onChange={(event) => setIncludeCancelled(event.target.checked)} className="h-4 w-4 rounded border-slate-300" />显示取消</label><button type="button" disabled={!selectedSummonID || refreshOnlineMutation.isPending} onClick={() => refreshOnlineMutation.mutate()} className="pp-button">{refreshOnlineMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <RefreshCw size={14} />}刷新在线位置</button></div></div>

            <div className="mt-4 grid gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 sm:grid-cols-4">
              <label><FieldLabel>PlayerUID</FieldLabel><input value={participantDraft.playerUID} onChange={(event) => setParticipantDraft((current) => ({ ...current, playerUID: event.target.value }))} className="pp-input w-full font-mono text-xs" placeholder="必填" /></label>
              <label><FieldLabel>昵称</FieldLabel><input value={participantDraft.nickname} onChange={(event) => setParticipantDraft((current) => ({ ...current, nickname: event.target.value }))} className="pp-input w-full" /></label>
              <label><FieldLabel>SteamID</FieldLabel><input value={participantDraft.steamID} onChange={(event) => setParticipantDraft((current) => ({ ...current, steamID: event.target.value }))} className="pp-input w-full font-mono text-xs" /></label>
              <div className="flex items-end"><button type="button" disabled={!selectedSummonID || !participantDraft.playerUID.trim() || registerMutation.isPending} onClick={() => registerMutation.mutate()} className="pp-btn pp-btn--primary w-full justify-center">{registerMutation.isPending ? <LoaderCircle className="animate-spin" size={14} /> : <UserPlus size={14} />}添加并读取位置</button></div>
            </div>

            {snapshotQuery.error && <div className="mt-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm font-semibold text-rose-700">{getErrorMessage(snapshotQuery.error)}</div>}
            <div className="mt-4 overflow-x-auto rounded-xl border border-slate-200">
              <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
                <thead className="bg-slate-50 text-[10px] font-black uppercase tracking-wider text-slate-400"><tr><th className="px-3 py-3">玩家</th><th className="px-3 py-3">状态</th><th className="px-3 py-3">区域</th><th className="px-3 py-3">位置</th><th className="px-3 py-3">时间</th><th className="px-3 py-3 text-right">操作</th></tr></thead>
                <tbody className="divide-y divide-slate-100 bg-white">
                  {participants.map((participant) => <tr key={participant.id}>
                    <td className="px-3 py-3"><div className="font-black text-slate-800">{participant.nickname || '未命名玩家'}</div><div className="mt-1 max-w-64 truncate font-mono text-[10px] text-slate-400">{participant.player_uid}</div>{participant.steam_id && <div className="mt-0.5 font-mono text-[10px] text-slate-400">{participant.steam_id}</div>}</td>
                    <td className="px-3 py-3"><span className={`rounded-full px-2.5 py-1 text-[10px] font-bold ${participantStatusClass[participant.status]}`}>{participantStatusLabel[participant.status]}</span></td>
                    <td className="px-3 py-3"><div className={`font-bold ${participant.eligible ? 'text-emerald-700' : participant.area_status === 'outside' ? 'text-rose-700' : 'text-amber-700'}`}>{areaStatusLabel[participant.area_status]}</div><div className="mt-1 text-[10px] text-slate-400">距离 {number.format(participant.distance)}</div></td>
                    <td className="px-3 py-3 font-mono text-[10px] text-slate-500">{participant.location ? `${number.format(participant.location.x)}, ${number.format(participant.location.y)}, ${number.format(participant.location.z)}` : '未取得'}</td>
                    <td className="px-3 py-3 text-[10px] text-slate-500"><div>报名 {formatTime(participant.registered_at)}</div>{participant.checked_at && <div className="mt-1">核验 {formatTime(participant.checked_at)}</div>}</td>
                    <td className="px-3 py-3"><div className="flex justify-end gap-1.5">{participant.status !== 'cancelled' && <button type="button" disabled={checkMutation.isPending} onClick={() => checkMutation.mutate(participant)} className="pp-button"><MapPin size={13} />核验位置</button>}{participant.status !== 'cancelled' && <button type="button" disabled={cancelMutation.isPending} onClick={() => { if (window.confirm(`取消 ${participant.nickname || participant.player_uid} 的报名？`)) cancelMutation.mutate(participant); }} className="rounded-lg border border-rose-100 px-2.5 py-2 font-bold text-rose-600 hover:bg-rose-50"><UserMinus size={13} /></button>}</div></td>
                  </tr>)}
                  {!snapshotQuery.isLoading && participants.length === 0 && <tr><td colSpan={6} className="px-4 py-10 text-center text-sm text-slate-400">暂无报名参与者。</td></tr>}
                </tbody>
              </table>
            </div>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm">
            <h2 className="font-black text-slate-900">报名审计</h2>
            <p className="mt-1 text-xs text-slate-400">显示该场次最近 200 条报名、区域核验和策略重算事件。</p>
            <div className="mt-4 space-y-2">
              {events.map((event) => <div key={event.id} className="rounded-xl bg-slate-50 p-3 text-xs">
                <div className="flex flex-wrap items-center gap-2"><span className="font-black text-slate-700">{eventLabel[event.event_type] || event.event_type}</span><span className="font-mono text-[10px] text-slate-400">{event.player_uid}</span><span className="ml-auto text-[10px] text-slate-400">{formatTime(event.created_at)}</span></div>
                <div className="mt-1 text-[10px] text-slate-500">{event.actor || '系统'} · {JSON.stringify(event.details || {})}</div>
              </div>)}
              {!snapshotQuery.isLoading && events.length === 0 && <div className="rounded-xl bg-slate-50 py-8 text-center text-xs text-slate-400">暂无报名审计事件。</div>}
            </div>
          </div>

          <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-xs leading-5 text-amber-800">
            <div className="flex gap-2"><CircleAlert className="mt-0.5 shrink-0" size={15} /><div><strong className="block">位置读取条件</strong>PalPanelBridge 必须为 0.1.24 或更高版本，`config.ini` Token 必须可用，并且玩家处于在线状态。位置读取失败不会自动取消已有报名。</div></div>
          </div>
        </div>
      </section>
    </div>
  );
};
