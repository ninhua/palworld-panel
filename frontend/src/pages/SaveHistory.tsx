import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, FileDiff, History, RefreshCw, Search } from 'lucide-react';
import { saveHistoryApi, type SaveHistoryChange, type SaveHistoryDiffSummary, type SaveHistorySnapshot } from '../api/saveHistory';
import { getErrorMessage } from '../api/client';

const categories = [
  { value: 'all', label: '全部变化' },
  { value: 'players', label: '玩家' },
  { value: 'guilds', label: '公会' },
  { value: 'bases', label: '基地' },
  { value: 'pals', label: '帕鲁' },
  { value: 'containers', label: '容器' },
  { value: 'items', label: '物品总量' },
];

const categoryLabels: Record<string, string> = Object.fromEntries(categories.map((item) => [item.value, item.label]));
const kindLabels: Record<string, string> = {
  added: '新增', removed: '移除', changed: '修改', increased: '增加', decreased: '减少',
};
const fieldLabels: Record<string, string> = {
  nickname: '名称', level: '等级', guild: '公会', last_online: '最后在线', owner: '归属', members: '成员',
  bases: '基地', location: '位置', structures: '建筑数', workers: '工作帕鲁', containers: '容器', status: '状态',
  character: '帕鲁种类', container: '容器', slot: '槽位', location_type: '位置类型', rank: '星级', ivs: '个体值',
  passives: '被动词条', owner_type: '归属类型', contents: '内容摘要', count: '数量',
};

export const SaveHistory: React.FC = () => {
  const history = useQuery({ queryKey: ['save-history'], queryFn: saveHistoryApi.list });
  const [fromID, setFromID] = useState('');
  const [toID, setToID] = useState('');
  const [category, setCategory] = useState('all');
  const [query, setQuery] = useState('');
  const [selectionSourceID, setSelectionSourceID] = useState('');
  const sourceID = history.data?.source.id || '';

  useEffect(() => {
    const items = history.data?.items || [];
    if (sourceID !== selectionSourceID) {
      setSelectionSourceID(sourceID);
      setToID(items[0]?.id || '');
      setFromID(items[1]?.id || '');
      return;
    }
    const ids = new Set(items.map((item) => item.id));
    const nextTo = toID && ids.has(toID) ? toID : items[0]?.id || '';
    const nextFrom = fromID && ids.has(fromID) && fromID !== nextTo ? fromID : items.find((item) => item.id !== nextTo)?.id || '';
    if (nextTo !== toID) setToID(nextTo);
    if (nextFrom !== fromID) setFromID(nextFrom);
  }, [fromID, history.data?.items, selectionSourceID, sourceID, toID]);

  const diff = useQuery({
    queryKey: ['save-history-diff', sourceID, fromID, toID, category, query],
    queryFn: () => saveHistoryApi.diff({ from: fromID, to: toID, category, q: query.trim(), limit: 200, offset: 0 }),
    enabled: Boolean(fromID && toID && fromID !== toID),
  });

  const snapshots = history.data?.items || [];
  const summaryCards = useMemo(() => summarize(diff.data?.summary), [diff.data?.summary]);
  const notice = history.error ? getErrorMessage(history.error) : diff.error ? getErrorMessage(diff.error) : '';

  return (
    <div className="page-shell">
      <div className="page-titlebar">
        <div>
          <p className="eyebrow">Save history</p>
          <h1>存档差异</h1>
          <p>比较同一存档源的两次成功索引，查看玩家、公会、基地、帕鲁、容器和物品总量变化。</p>
        </div>
        <button type="button" className="pp-button" onClick={() => void history.refetch()} disabled={history.isFetching}>
          <RefreshCw className={history.isFetching ? 'animate-spin' : ''} size={15} />刷新
        </button>
      </div>

      <section className="status-strip compact-status">
        <div className="server-hero">
          <div>
            <span className="eyebrow">当前存档源</span>
            <h2>{history.data?.source.name || '等待索引'}</h2>
            <p>{snapshots.length} 份快照 · {formatBytes(history.data?.total_bytes || 0)} / {formatBytes(history.data?.max_total_bytes || 0)}</p>
          </div>
          <span className={`state-pill ${snapshots.length >= 2 ? 'ok' : 'warn'}`}>{snapshots.length >= 2 ? '可比较' : '需要两份快照'}</span>
        </div>
        <Metric label="保留上限" value={history.data?.retention || 24} suffix="份" />
        <Metric label="当前快照" value={snapshots.length} suffix="份" />
        <Metric label="差异结果" value={diff.data?.total || 0} suffix="项" />
      </section>

      {notice && <div className="pp-notice">{notice}</div>}
      {!history.isLoading && snapshots.length < 2 && (
        <div className="pp-notice">首次打开已记录当前成功索引。存档变化后执行“存档中心 → 重建”或等待自动索引，形成第二份快照后即可比较。</div>
      )}

      <section className="pp-card">
        <div className="pp-card-head"><div><h2>比较范围</h2><p>快照按当前激活存档源隔离；切换存档源后只显示该世界自己的历史。</p></div><History size={18} /></div>
        <div className="content-grid two-column">
          <label className="field-label">较早快照
            <select aria-label="较早快照" value={fromID} onChange={(event) => setFromID(event.target.value)}>
              <option value="">选择快照</option>
              {snapshots.map((item) => <option key={item.id} value={item.id}>{snapshotLabel(item)}</option>)}
            </select>
          </label>
          <label className="field-label">较新快照
            <select aria-label="较新快照" value={toID} onChange={(event) => setToID(event.target.value)}>
              <option value="">选择快照</option>
              {snapshots.map((item) => <option key={item.id} value={item.id}>{snapshotLabel(item)}</option>)}
            </select>
          </label>
          <label className="field-label">变化类别
            <select aria-label="变化类别" value={category} onChange={(event) => setCategory(event.target.value)}>
              {categories.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
            </select>
          </label>
          <label className="field-label">搜索
            <span className="input-with-icon"><Search size={15} /><input aria-label="搜索差异" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="名称、ID、字段或物品 ID" /></span>
          </label>
        </div>
      </section>

      {fromID && toID && fromID === toID && <div className="pp-notice">请选择两份不同的快照。</div>}
      {fromID && toID && fromID !== toID && diff.isLoading && <div className="pp-notice">正在计算差异……</div>}

      {diff.data && fromID !== toID && (
        <>
          <section className="status-strip compact-status">
            {summaryCards.map((item) => <Metric key={item.label} label={item.label} value={item.value} suffix="项" />)}
          </section>
          <section className="pp-card">
            <div className="pp-card-head">
              <div><h2>变化明细</h2><p>{snapshotShort(diff.data.from)} <ArrowRight size={13} /> {snapshotShort(diff.data.to)} · 当前筛选 {diff.data.total} 项，最多显示 200 项。</p></div>
              <FileDiff size={18} />
            </div>
            {diff.isFetching && <div className="pp-notice">正在计算差异……</div>}
            {!diff.isFetching && diff.data.items.length === 0 && <div className="empty-state">所选范围没有匹配的变化。</div>}
            <div className="source-list">
              {diff.data.items.map((change, index) => <ChangeRow key={`${change.category}-${change.kind}-${change.id}-${index}`} change={change} />)}
            </div>
          </section>
        </>
      )}
    </div>
  );
};

const ChangeRow: React.FC<{ change: SaveHistoryChange }> = ({ change }) => (
  <article className="source-row">
    <span className={`state-pill ${change.kind === 'added' || change.kind === 'increased' ? 'ok' : change.kind === 'removed' || change.kind === 'decreased' ? 'warn' : ''}`}>
      {kindLabels[change.kind] || change.kind}
    </span>
    <div className="source-copy">
      <strong>{change.label || change.id}</strong>
      <span>{categoryLabels[change.category] || change.category} · {change.id}{typeof change.delta === 'number' ? ` · Δ ${change.delta > 0 ? '+' : ''}${change.delta}` : ''}</span>
      {change.fields.map((field) => (
        <span key={`${change.id}-${field.field}`}><b>{fieldLabels[field.field] || field.field}</b>：{field.before || '—'} <ArrowRight size={12} /> {field.after || '—'}</span>
      ))}
    </div>
  </article>
);

const Metric: React.FC<{ label: string; value: number; suffix: string }> = ({ label, value, suffix }) => (
  <div className="metric"><span className="eyebrow">{label}</span><strong>{value}</strong><span>{suffix}</span></div>
);

const summarize = (summary?: SaveHistoryDiffSummary) => {
  if (!summary) return [];
  return [
    { label: '玩家', value: summary.players_added + summary.players_removed + summary.players_changed },
    { label: '公会', value: summary.guilds_added + summary.guilds_removed + summary.guilds_changed },
    { label: '基地', value: summary.bases_added + summary.bases_removed + summary.bases_changed },
    { label: '帕鲁', value: summary.pals_added + summary.pals_removed + summary.pals_changed },
    { label: '容器', value: summary.containers_added + summary.containers_removed + summary.containers_changed },
    { label: '物品', value: summary.items_increased + summary.items_decreased },
  ];
};

const snapshotLabel = (item: SaveHistorySnapshot) => `${new Date(item.generated_at || item.captured_at).toLocaleString('zh-CN')} · ${item.counts.players} 玩家 · ${item.fingerprint.slice(0, 8)}`;
const snapshotShort = (item: SaveHistorySnapshot) => new Date(item.generated_at || item.captured_at).toLocaleString('zh-CN');
const formatBytes = (value: number) => value >= 1024 * 1024 ? `${(value / 1024 / 1024).toFixed(1)} MiB` : value >= 1024 ? `${(value / 1024).toFixed(1)} KiB` : `${value} B`;
