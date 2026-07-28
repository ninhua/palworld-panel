import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  DownloadCloud,
  Eye,
  EyeOff,
  ExternalLink,
  FileArchive,
  FolderSearch,
  Info,
  Languages,
  LogIn,
  PackageCheck,
  Power,
  RefreshCw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Trash2,
  UploadCloud,
  Wrench,
  X,
} from 'lucide-react';
import { ApiError, getErrorMessage } from '../api/client';
import { modsApi } from '../api/mods';
import { securityApi } from '../api/security';
import { serverApi } from '../api/server';
import { tasksApi } from '../api/tasks';
import type { ImportInspection, Job, LocalModAction, LocalModFinding, LocalScanResult, ModItem, PalDefenderStatus, SteamWorkshopAuthStatus, WorkshopItem } from '../types';
import { DataTable } from '../components/ui/DataTable';
import { ModalPortal } from '../components/ui/ModalPortal';
import { StatusBadge } from '../components/ui/StatusBadge';
import { ModConfigWorkspace } from '../components/mods/ModConfigWorkspace';
import { useServerStore } from '../store/useServerStore';
import { useI18n, type TranslationKey } from '../i18n';

type ModsTab = 'store' | 'installed' | 'local' | 'config';

const tabs: Array<{ id: ModsTab; labelKey: TranslationKey }> = [
  { id: 'store', labelKey: 'mods.store' },
  { id: 'installed', labelKey: 'mods.installed' },
  { id: 'local', labelKey: 'mods.local' },
  { id: 'config', labelKey: 'mods.config' },
];

const sortOptions = [
  { id: 'popular', labelKey: 'mods.popular' as TranslationKey },
  { id: 'trend', labelKey: 'mods.trending' as TranslationKey },
  { id: 'new', labelKey: 'mods.new' as TranslationKey },
  { id: 'updated', labelKey: 'mods.updated' as TranslationKey },
];

const isSteamLoginRequired = (error: unknown) => error instanceof ApiError && [
  'steam_login_required',
  'steam_guard_required',
  'steam_mobile_confirmation_required',
  'invalid_steam_credentials',
].includes(error.code || '');

const isWorkshopImportSource = (source: string) => {
  const value = source.trim();
  return /^\d+$/.test(value) || /^https?:\/\/(?:www\.)?steamcommunity\.com\/sharedfiles\/filedetails\//i.test(value);
};

export const Mods: React.FC = () => {
  const { t } = useI18n();
  const { session } = useServerStore();
  const canAuthenticateSteam = Boolean(session?.permissions.includes('security:write'));
  const [activeTab, setActiveTab] = useState<ModsTab>('store');
  const [mods, setMods] = useState<ModItem[]>([]);
  const [securityStatus, setSecurityStatus] = useState<PalDefenderStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [installedSearch, setInstalledSearch] = useState('');
  const [importOpen, setImportOpen] = useState(false);
  const [activeJob, setActiveJob] = useState<Job | null>(null);
  const [pendingRestart, setPendingRestart] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const [storeQuery, setStoreQuery] = useState('');
  const [storeSort, setStoreSort] = useState('popular');
  const [tagText, setTagText] = useState('');
  const [storeItems, setStoreItems] = useState<WorkshopItem[]>([]);
  const [storeTotal, setStoreTotal] = useState(0);
  const [storeNextCursor, setStoreNextCursor] = useState<string | undefined>();
  const [storeLoading, setStoreLoading] = useState(false);
  const [storeError, setStoreError] = useState<string | null>(null);
  const [workshopAuth, setWorkshopAuth] = useState<SteamWorkshopAuthStatus | null>(null);
  const [workshopAuthLoading, setWorkshopAuthLoading] = useState(true);
  const [workshopAuthError, setWorkshopAuthError] = useState<string | null>(null);
  const [workshopLoginOpen, setWorkshopLoginOpen] = useState(false);
  const [selectedWorkshop, setSelectedWorkshop] = useState<WorkshopItem | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [translationLoading, setTranslationLoading] = useState(false);
  const [translationError, setTranslationError] = useState<string | null>(null);
  const [localScan, setLocalScan] = useState<LocalScanResult | null>(null);
  const [localScanLoading, setLocalScanLoading] = useState(false);
  const [localScanError, setLocalScanError] = useState<string | null>(null);
  const [localScanSearch, setLocalScanSearch] = useState('');
  const [localActionBusy, setLocalActionBusy] = useState<string | null>(null);
  const initialLoadRef = useRef(false);
  const localScanRequestedRef = useRef(false);

  const loadInstalled = useCallback(async () => {
    setLoading(true);
    try {
      const [list, status, nextSecurityStatus] = await Promise.all([
        modsApi.list(),
        serverApi.getStatus(),
        securityApi.status(),
      ]);
      setMods(Array.isArray(list) ? list : []);
      setPendingRestart(status.pending_restart);
      setSecurityStatus(nextSecurityStatus);
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  }, []);

  const loadWorkshopAuthStatus = useCallback(async () => {
    setWorkshopAuthLoading(true);
    setWorkshopAuthError(null);
    try {
      const status = await modsApi.workshopAuthStatus();
      setWorkshopAuth(status);
      return status;
    } catch (error) {
      setWorkshopAuthError(getErrorMessage(error));
      return null;
    } finally {
      setWorkshopAuthLoading(false);
    }
  }, []);

  const handleWorkshopAuthFailure = useCallback((error: unknown) => {
    if (!isSteamLoginRequired(error)) return false;
    const errorMessage = getErrorMessage(error);
    setWorkshopAuth((current) => current
      ? { ...current, logged_in: false, login_in_progress: false, verification_required: true, message: errorMessage }
      : {
          supported: true,
          steamcmd_installed: true,
          credentials_secure: false,
          login_in_progress: false,
          logged_in: false,
          verification_required: true,
          password_configured: false,
          steam_guard_required: error instanceof ApiError && error.code === 'steam_guard_required',
          message: errorMessage,
        });
    setWorkshopAuthError(errorMessage);
    setWorkshopLoginOpen(true);
    return true;
  }, []);

  const runLocalScan = useCallback(async () => {
    setLocalScanLoading(true);
    setLocalScanError(null);
    try {
      setLocalScan(await modsApi.scanLocal());
    } catch (error) {
      setLocalScanError(getErrorMessage(error));
    } finally {
      setLocalScanLoading(false);
    }
  }, []);

  const actOnLocalFinding = async (finding: LocalModFinding, action: LocalModAction) => {
    const capability = finding.actions.find((item) => item.action === action);
    if (!capability?.available) return;
    const confirmed = action !== 'delete' || window.confirm(`删除 PalPanel 管理的 Mod“${finding.name}”及其数据库记录？此操作不可撤销。`);
    if (!confirmed) return;
    setLocalActionBusy(`${finding.id}:${action}`);
    setLocalScanError(null);
    try {
      const result = await modsApi.actOnLocalFinding(finding, action, action === 'delete');
      setLocalScan(result.scan);
      setMessage(result.message);
      if (action === 'import' || action === 'repair' || action === 'delete') {
        await loadInstalled();
      }
    } catch (error) {
      setLocalScanError(getErrorMessage(error));
    } finally {
      setLocalActionBusy(null);
    }
  };

  const loadStore = useCallback(async (
    reset = true,
    overrides: { sort?: string } = {},
  ) => {
    setStoreLoading(true);
    setStoreError(null);
    try {
      const response = await modsApi.searchWorkshop({
        q: storeQuery.trim(),
        sort: overrides.sort || storeSort,
        cursor: reset ? undefined : storeNextCursor,
        page_size: 24,
        tags: tagText
          .split(',')
          .map((tag) => tag.trim())
          .filter(Boolean),
      });
      setStoreItems((current) => (reset ? response.items : [...current, ...response.items]));
      setStoreNextCursor(response.next_cursor);
      setStoreTotal(response.total);
    } catch (error) {
      if (!handleWorkshopAuthFailure(error)) {
        setStoreError(getErrorMessage(error));
      }
      if (reset) {
        setStoreItems([]);
        setStoreNextCursor(undefined);
        setStoreTotal(0);
      }
    } finally {
      setStoreLoading(false);
    }
  }, [handleWorkshopAuthFailure, storeNextCursor, storeQuery, storeSort, tagText]);

  useEffect(() => {
    if (initialLoadRef.current) return;
    initialLoadRef.current = true;
    void loadInstalled();
    void loadWorkshopAuthStatus();
    void loadStore(true);
  }, [loadInstalled, loadStore, loadWorkshopAuthStatus]);

  useEffect(() => {
    if ((activeTab !== 'local' && activeTab !== 'config') || localScanRequestedRef.current) return;
    localScanRequestedRef.current = true;
    void runLocalScan();
  }, [activeTab, runLocalScan]);

  const storeStatusByWorkshopID = useMemo(() => {
    const map = new Map<string, WorkshopItem>();
    for (const item of storeItems) {
      map.set(item.id, item);
    }
    return map;
  }, [storeItems]);

  const filteredInstalled = useMemo(() => {
    const keyword = installedSearch.trim().toLowerCase();
    if (!keyword) return mods;
    return mods.filter(
      (mod) =>
        mod.name.toLowerCase().includes(keyword) ||
        mod.package_name.toLowerCase().includes(keyword) ||
        mod.id.toLowerCase().includes(keyword) ||
        String(mod.workshop_id || '').toLowerCase().includes(keyword),
    );
  }, [mods, installedSearch]);

  const filteredLocalFindings = useMemo(() => {
    const findings = localScan?.findings || [];
    const keyword = localScanSearch.trim().toLowerCase();
    if (!keyword) return findings;
    return findings.filter((finding) =>
      [
        finding.name,
        finding.package_name || '',
        finding.version || '',
        finding.source,
        finding.ownership,
        finding.confidence,
        finding.state,
        ...finding.paths,
        ...(finding.issues || []),
      ].some((value) => value.toLowerCase().includes(keyword)),
    );
  }, [localScan, localScanSearch]);

  const trackJob = async (job: Job) => {
    setActiveJob(job);
    const done = await tasksApi.waitForJob(job.id, setActiveJob);
    setMessage(done.status === 'success' ? '任务已完成，重启后生效' : done.error || '任务失败');
    if (done.error_code && ['steam_login_required', 'steam_guard_required', 'steam_mobile_confirmation_required', 'invalid_steam_credentials'].includes(done.error_code)) {
      setWorkshopAuth((current) => current ? {
        ...current,
        logged_in: false,
        verification_required: true,
        steam_guard_required: done.error_code === 'steam_guard_required',
        message: done.error || done.message,
      } : current);
      setWorkshopAuthError(done.error || done.message || 'Steam 登录失败');
      setWorkshopLoginOpen(true);
    }
    await loadInstalled();
    return done;
  };

  const installWorkshop = async (itemID: string, enable: boolean) => {
    if (!workshopAuth?.logged_in && !workshopAuth?.password_configured) {
      setWorkshopLoginOpen(true);
      return;
    }
    if (!itemID.trim()) {
      setMessage('请输入 Steam Workshop Item ID');
      return;
    }
    try {
      const job = await modsApi.downloadWorkshop(itemID.trim(), enable);
      const done = await trackJob(job);
      if (done.status === 'success' && !storeError) {
        await loadStore(true);
      }
    } catch (error) {
      if (!handleWorkshopAuthFailure(error)) setMessage(getErrorMessage(error));
    }
  };

  const toggleMod = async (mod: ModItem) => {
    try {
      await modsApi.setEnabled(mod.id, !mod.enabled);
      setMessage(`${mod.name} 已${mod.enabled ? '禁用' : '启用'}，重启后生效`);
      await loadInstalled();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const deleteMod = async (mod: ModItem) => {
    if (!window.confirm(`删除 Mod "${mod.name}"？`)) return;
    try {
      await modsApi.delete(mod.id);
      setMessage('Mod 已删除，重启后生效');
      await loadInstalled();
      if (!storeError) {
        await loadStore(true);
      }
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const openWorkshopDetail = async (item: WorkshopItem) => {
    setSelectedWorkshop(item);
    setTranslationError(null);
    setDetailLoading(true);
    try {
      setSelectedWorkshop(await modsApi.getWorkshopItem(item.id));
    } catch (error) {
      if (!handleWorkshopAuthFailure(error)) setMessage(getErrorMessage(error));
    } finally {
      setDetailLoading(false);
    }
  };

  const translateSelectedWorkshop = async (force: boolean) => {
    if (!selectedWorkshop) return;
    const workshopID = selectedWorkshop.id;
    setTranslationLoading(true);
    setTranslationError(null);
    try {
      const translation = await modsApi.translateWorkshop(workshopID, force);
      setSelectedWorkshop((current) => current?.id === workshopID ? { ...current, translation } : current);
    } catch (error) {
      if (handleWorkshopAuthFailure(error)) return;
      const errorMessage = getErrorMessage(error);
      setTranslationError(errorMessage);
      setMessage(errorMessage);
    } finally {
      setTranslationLoading(false);
    }
  };

  const workshopAuthVerified = async (status: SteamWorkshopAuthStatus) => {
    setWorkshopAuth(status);
    setWorkshopAuthError(null);
    setWorkshopLoginOpen(false);
  };

  const headers = [
    { key: 'name', label: 'Mod' },
    { key: 'package', label: 'PackageName' },
    { key: 'source', label: '来源' },
    { key: 'status', label: '状态' },
    { key: 'updated', label: '更新' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];
  const localHeaders = [
    { key: 'name', label: 'Mod' },
    { key: 'source', label: '来源' },
    { key: 'confidence', label: '置信度' },
    { key: 'status', label: '状态' },
    { key: 'paths', label: '路径' },
    { key: 'actions', label: '操作', align: 'center' as const },
  ];
  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-5 p-4 sm:p-6 lg:p-8">
	  <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
		<div className="flex max-w-full items-center overflow-x-auto rounded-lg border border-slate-200 bg-white p-1">
		  {tabs.map((tab) => {
			const active = activeTab === tab.id;
			return (
			  <button
				key={tab.id}
				type="button"
				onClick={() => setActiveTab(tab.id)}
				className={`shrink-0 rounded-md px-4 py-2 text-xs font-bold transition-all ${
				  active ? 'bg-sky-100 text-sky-800 ring-1 ring-sky-200' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-800'
				}`}
			  >
				{t(tab.labelKey)}
			  </button>
			);
		  })}
		</div>
		<button type="button" onClick={() => setImportOpen(true)} className="inline-flex items-center justify-center gap-2 rounded-lg bg-sky-500 px-4 py-2.5 text-xs font-bold text-white hover:bg-sky-600">
		  <DownloadCloud size={15} />{t('mods.import')}
		</button>
      </div>

      {pendingRestart && (
        <div className="rounded-lg border border-amber-100 bg-amber-50 px-5 py-4">
          <p className="text-xs font-bold text-amber-800">{t('mods.pendingRestart')}</p>
        </div>
      )}
      {message && <div className="rounded-lg border border-sky-100 bg-sky-50 px-5 py-3 text-xs font-semibold text-sky-700">{message}</div>}
      {activeJob && <JobProgress job={activeJob} />}

      {activeTab === 'store' && (
        <section className="flex flex-col gap-4">
          <div className={`flex flex-col gap-3 rounded-lg border px-4 py-3 sm:flex-row sm:items-center sm:justify-between ${workshopAuth?.logged_in || workshopAuth?.password_configured ? 'border-emerald-100 bg-emerald-50' : 'border-amber-100 bg-amber-50'}`}>
            <div className="min-w-0">
              <p className="text-xs font-bold text-slate-800">
                {workshopAuthLoading
                  ? '正在读取 Steam 登录配置…'
                  : workshopAuth?.logged_in || workshopAuth?.password_configured
                    ? `Workshop 下载账号：${workshopAuth.account_name || '已配置'}`
                    : '浏览无需登录，下载 Workshop Mod 前需要配置 Steam 凭据'}
              </p>
              {(workshopAuthError || workshopAuth?.message) && (
                <p className="mt-1 text-[11px] font-semibold leading-5 text-slate-600">{localizeSteamAuthMessage(workshopAuthError || workshopAuth?.message)}</p>
              )}
            </div>
            {canAuthenticateSteam && workshopAuth?.supported !== false && (
              <button type="button" onClick={() => setWorkshopLoginOpen(true)} className="inline-flex shrink-0 items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-xs font-bold text-white hover:bg-slate-800">
                <LogIn size={14} />
                {workshopAuth?.password_configured ? '管理 Steam 凭据' : '配置 Steam 登录'}
              </button>
            )}
          </div>
          <div className="rounded-lg border border-slate-100 bg-white p-4">
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,1fr)_220px_auto]">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} />
                <input
                  type="search"
                  value={storeQuery}
                  onChange={(event) => setStoreQuery(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') void loadStore(true);
                  }}
                  placeholder={t('mods.searchWorkshop')}
                  className="w-full rounded-lg border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50 disabled:text-slate-400"
                />
              </div>
              <div className="relative">
                <SlidersHorizontal className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={15} />
                <input
                  type="text"
                  value={tagText}
                  onChange={(event) => setTagText(event.target.value)}
                  placeholder={t('mods.tags')}
                  className="w-full rounded-lg border border-slate-200 py-2.5 pl-9 pr-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50 disabled:text-slate-400"
                />
              </div>
              <button
                type="button"
                onClick={() => loadStore(true)}
                disabled={storeLoading}
                className="inline-flex items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-xs font-bold text-white hover:bg-slate-800 disabled:opacity-50"
              >
                <Search size={14} />
                {t('mods.search')}
              </button>
            </div>
            <div className="mt-3 flex max-w-full items-center gap-2 overflow-x-auto">
              {sortOptions.map((option) => {
                const active = storeSort === option.id;
                return (
                  <button
                    key={option.id}
                    type="button"
                    onClick={() => {
                      setStoreSort(option.id);
                      void loadStore(true, { sort: option.id });
                    }}
                    className={`shrink-0 rounded-md border px-3 py-1.5 text-[11px] font-bold disabled:opacity-50 ${
                      active ? 'border-sky-200 bg-sky-50 text-sky-700' : 'border-slate-200 text-slate-500 hover:bg-slate-50'
                    }`}
                  >
                    {t(option.labelKey)}
                  </button>
                );
              })}
            </div>
          </div>

          {storeError && <div className="rounded-lg border border-rose-100 bg-rose-50 px-5 py-4 text-xs font-bold text-rose-700">{storeError}</div>}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            {storeItems.map((item) => (
              <WorkshopCard
                key={item.id}
                item={item}
                onDetail={() => openWorkshopDetail(item)}
                onInstall={() => installWorkshop(item.id, false)}
                onInstallEnabled={() => installWorkshop(item.id, true)}
              />
            ))}
          </div>

          {storeLoading && (
            <div className="py-10 text-center text-xs font-semibold text-slate-400">
              <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
              {t('mods.loadingWorkshop')}
            </div>
          )}
          {!storeLoading && !storeError && storeItems.length === 0 && (
            <div className="rounded-lg border border-dashed border-slate-200 bg-white px-4 py-12 text-center text-xs font-semibold text-slate-400">
              {t('mods.noMatches')}
            </div>
          )}
          {storeNextCursor && (
            <div className="flex justify-center">
              <button
                type="button"
                onClick={() => loadStore(false)}
                disabled={storeLoading}
                className="rounded-lg border border-slate-200 bg-white px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:opacity-50"
              >
                {t('mods.loadMore')}{storeTotal > 0 ? ` (${storeItems.length}/${storeTotal})` : ''}
              </button>
            </div>
          )}
        </section>
      )}

      {activeTab === 'local' && (
        <section className="rounded-lg border border-slate-100 bg-white p-4 sm:p-6">
          <div className="flex flex-col gap-3 border-b border-slate-100 pb-5 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <h2 className="text-[15px] font-bold text-slate-800">{t('mods.localScan')}</h2>
              <p className="mt-1 break-all font-mono text-[10px] font-semibold text-slate-400" title={localScan?.server_dir || undefined}>
                {localScan?.server_dir || t('mods.waitingScan')}
              </p>
            </div>
            <button
              type="button"
              onClick={() => void runLocalScan()}
              disabled={localScanLoading}
              className="inline-flex shrink-0 items-center justify-center gap-2 rounded-lg border border-slate-200 bg-white px-4 py-2.5 text-xs font-bold text-slate-700 hover:bg-slate-50 disabled:opacity-50"
            >
              <RefreshCw size={14} className={localScanLoading ? 'animate-spin' : ''} />
              {t('mods.rescan')}
            </button>
          </div>

          <div className="my-4 flex items-start gap-3 rounded-lg border border-sky-100 bg-sky-50 px-4 py-3 text-xs font-semibold leading-5 text-sky-800">
            <FolderSearch size={16} className="mt-0.5 shrink-0" />
            <p>每次操作都会重新扫描并校验结果修订。未知、链接或不完整文件不会自动导入或删除；删除仅限可验证的 PalPanel 管理目录并要求明确确认。</p>
          </div>

          {localScanError && (
            <div role="alert" className="mb-4 rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-bold text-rose-700">
              {localScanError}
            </div>
          )}

          {localScan && (
            <div className="mb-5 grid gap-3 border-b border-slate-100 pb-5 sm:grid-cols-3">
              <LocalScanMetric label="检测结果" value={`${localScan.findings.length} 项`} />
              <LocalScanMetric label="跳过路径" value={`${localScan.skipped_paths.length} 项`} />
              <LocalScanMetric label="扫描时间" value={formatLocalScanTime(localScan.scanned_at)} />
              {localScan.warnings.length > 0 && (
                <div className="rounded-lg border border-amber-100 bg-amber-50 px-3 py-2 sm:col-span-3">
                  <p className="text-[10px] font-bold text-amber-700">扫描警告</p>
                  <ul className="mt-1 grid gap-1 text-[11px] font-semibold text-amber-800">
                    {localScan.warnings.map((warning) => <li key={warning} className="break-words">{warning}</li>)}
                  </ul>
                </div>
              )}
              {localScan.skipped_paths.length > 0 && (
                <details className="rounded-lg border border-slate-100 bg-slate-50 px-3 py-2 sm:col-span-3">
                  <summary className="cursor-pointer text-[11px] font-bold text-slate-600">查看已跳过路径（{localScan.skipped_paths.length}）</summary>
                  <ul className="mt-2 grid gap-1 font-mono text-[10px] font-semibold text-slate-500">
                    {localScan.skipped_paths.map((path) => <li key={path} className="break-all">{path}</li>)}
                  </ul>
                </details>
              )}
            </div>
          )}

          {localScanLoading && !localScan ? (
            <div className="py-12 text-center text-xs font-semibold text-slate-400">
              <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
              正在扫描本地 Mod...
            </div>
          ) : localScan ? (
            <DataTable
              title={`检测结果（${localScan.findings.length}）`}
              headers={localHeaders}
              data={filteredLocalFindings}
              searchText={localScanSearch}
              onSearchChange={setLocalScanSearch}
              searchPlaceholder="搜索名称、来源、状态或路径"
              emptyText="未检测到匹配的本地 Mod"
              renderCard={(finding) => (
                <LocalFindingCard
                  finding={finding}
                  canWrite={Boolean(session?.permissions.includes('mods:write'))}
                  busy={localActionBusy}
                  onAction={actOnLocalFinding}
                />
              )}
              renderRow={(finding, index) => (
                <tr key={`${finding.source}-${finding.name}-${index}`} className="align-top hover:bg-slate-50/50">
                  <td className="px-6 py-4">
                    <LocalFindingIdentity finding={finding} />
                  </td>
                  <td className="px-6 py-4">
                    <p className="text-xs font-bold text-slate-700">{localSourceText[finding.source]}</p>
                    <p className="mt-1 text-[10px] font-semibold text-slate-400">{localOwnershipText[finding.ownership]}</p>
                  </td>
                  <td className="px-6 py-4">
                    <ConfidenceBadge confidence={finding.confidence} />
                  </td>
                  <td className="px-6 py-4">
                    <LocalFindingState finding={finding} />
                  </td>
                  <td className="max-w-md px-6 py-4">
                    <LocalFindingPaths paths={finding.paths} />
                  </td>
                  <td className="px-6 py-4 text-center">
                    <LocalFindingActions
                      finding={finding}
                      canWrite={Boolean(session?.permissions.includes('mods:write'))}
                      busy={localActionBusy}
                      onAction={actOnLocalFinding}
                    />
                  </td>
                </tr>
              )}
            />
          ) : null}
        </section>
      )}

      {activeTab === 'installed' && (
        <section className="rounded-lg border border-slate-100 bg-white p-4 sm:p-6">
          <InstalledRuntimeComponents status={securityStatus} loading={loading} />
          {loading ? (
            <div className="py-12 text-center text-xs font-semibold text-slate-400">
              <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
              {t('mods.loadingInstalled')}
            </div>
          ) : (
            <DataTable
              title={t('mods.installedTitle', { count: mods.length })}
              headers={headers}
              data={filteredInstalled}
              searchText={installedSearch}
              onSearchChange={setInstalledSearch}
              searchPlaceholder="搜索 Mod 名称、PackageName 或 ID"
              emptyText="暂无 Mod"
              renderCard={(mod) => {
                const status = storeStatusByWorkshopID.get(mod.workshop_id || mod.id);
                return (
                  <ModCard
                    key={mod.id}
                    mod={mod}
                    updateAvailable={Boolean(status?.update_available)}
                    onToggle={() => toggleMod(mod)}
                    onDelete={() => deleteMod(mod)}
                    onUpdate={() => installWorkshop(mod.workshop_id || mod.id, mod.enabled)}
                  />
                );
              }}
              renderRow={(mod) => {
                const status = storeStatusByWorkshopID.get(mod.workshop_id || mod.id);
                const updateAvailable = Boolean(status?.update_available);
                return (
                  <tr key={mod.id} className="hover:bg-slate-50/50">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-3">
                        <ModPreview mod={mod} />
                        <div className="min-w-0">
                          <p className="truncate text-xs font-bold text-slate-700">{mod.name}</p>
                          <p className="truncate font-mono text-[10px] text-slate-400">{mod.workshop_id || mod.id}</p>
                        </div>
                      </div>
                    </td>
                    <td className="px-6 py-4 text-xs font-semibold text-slate-600">{mod.package_name || '-'}</td>
                    <td className="px-6 py-4 text-xs font-semibold text-slate-500">{mod.source === 'workshop' ? 'Workshop' : 'Upload'}</td>
                    <td className="px-6 py-4">
                      <StatusBadge status={mod.enabled ? 'enabled' : 'disabled'} />
                    </td>
                    <td className="px-6 py-4">
                      {updateAvailable ? <StatusBadge status="updating" customText="可更新" /> : <span className="text-xs font-medium text-slate-400">-</span>}
                    </td>
                    <td className="px-6 py-4 text-center">
                      <div className="flex justify-center gap-2">
                        {mod.source === 'workshop' && (
                          <button
                            type="button"
                            onClick={() => installWorkshop(mod.workshop_id || mod.id, mod.enabled)}
                            className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50"
                            aria-label="重新安装或更新 Mod"
                          >
                            <RefreshCw size={14} />
                          </button>
                        )}
                        <button
                          type="button"
                          onClick={() => toggleMod(mod)}
                          className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50"
                          aria-label="启用或禁用 Mod"
                        >
                          <Power size={14} />
                        </button>
                        <button
                          type="button"
                          onClick={() => deleteMod(mod)}
                          className="rounded-lg border border-rose-200 p-2 text-rose-500 hover:bg-rose-50"
                          aria-label="删除 Mod"
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              }}
            />
          )}
        </section>
      )}

      {activeTab === 'config' && (
        <ModConfigWorkspace
          mods={mods}
          localFindings={localScan?.findings || []}
          canWrite={Boolean(session?.permissions.includes('mods:write'))}
          canReloadPalDefender={Boolean(session?.permissions.includes('security:write'))}
        />
      )}

	  {importOpen && (
		<ImportDialog
		  onClose={() => setImportOpen(false)}
		  workshopAuthenticated={workshopAuth?.logged_in === true || workshopAuth?.password_configured === true}
		  onWorkshopAuthRequired={() => {
			setImportOpen(false);
			setWorkshopLoginOpen(true);
		  }}
		  onImport={async (job) => {
			setImportOpen(false);
			const done = await trackJob(job);
			if (done.status === 'success' && !storeError) await loadStore(true);
		  }}
		/>
	  )}
      {selectedWorkshop && (
        <WorkshopDrawer
          item={selectedWorkshop}
          loading={detailLoading}
          translationLoading={translationLoading}
          translationError={translationError}
          canTranslate={Boolean(session?.permissions.includes('mods:write'))}
          onClose={() => setSelectedWorkshop(null)}
          onTranslate={translateSelectedWorkshop}
          onInstall={() => installWorkshop(selectedWorkshop.id, false)}
          onInstallEnabled={() => installWorkshop(selectedWorkshop.id, true)}
        />
      )}
      {workshopLoginOpen && (
        <WorkshopLoginDialog
          status={workshopAuth}
          initialError={workshopAuthError}
          canAuthenticate={canAuthenticateSteam}
          onClose={() => setWorkshopLoginOpen(false)}
          onStatusChange={(status) => {
            setWorkshopAuth(status);
            setWorkshopAuthError(null);
          }}
          onVerified={workshopAuthVerified}
        />
      )}
    </div>
  );
};

const InstalledRuntimeComponents: React.FC<{
  status: PalDefenderStatus | null;
  loading: boolean;
}> = ({ status, loading }) => {
  const components = [
    {
      name: 'UE4SS',
      installed: Boolean(status?.ue4ss.installed),
      version: status?.ue4ss.version,
      loadVerified: Boolean(status?.ue4ss.load_verified),
      detail: status?.ue4ss.message,
    },
    {
      name: 'PalDefender',
      installed: Boolean(status?.installed),
      version: status?.version,
      loadVerified: Boolean(status?.load_verified),
      detail: status?.needs_first_start ? '需要启动一次服务器以生成并验证配置' : undefined,
    },
  ];

  return (
    <div className="mb-5 border-b border-slate-100 pb-5">
      <div className="mb-3 flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h2 className="text-[15px] font-bold text-slate-800">运行组件</h2>
          <p className="mt-1 text-[11px] font-semibold text-slate-500">
            UE4SS 与 PalDefender 根据本机文件和启动日志单独检测；PalDefender 安装前会先检查并安装 UE4SS。
          </p>
        </div>
        <span className="text-[10px] font-semibold text-slate-400">普通与手工 Mod 继续显示在下方列表和“本地检测”中</span>
      </div>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {components.map((component) => (
          <div key={component.name} className="rounded-lg border border-slate-200 bg-slate-50/70 p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <p className="text-sm font-bold text-slate-800">{component.name}</p>
                <p className="mt-1 truncate font-mono text-[10px] text-slate-400">
                  {loading ? '正在检测...' : component.version || '未检测到版本'}
                </p>
              </div>
              <StatusBadge
                status={loading ? 'running_job' : component.installed ? 'success' : 'missing'}
                customText={loading ? '检测中' : component.installed ? '已安装' : '未安装'}
              />
            </div>
            {!loading && component.installed && (
              <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] font-semibold text-slate-600">
                <StatusBadge
                  status={component.loadVerified ? 'success' : 'waiting'}
                  customText={component.loadVerified ? '启动日志已确认' : '待启动验证'}
                />
                {component.detail && <span className="min-w-0 break-words">{component.detail}</span>}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};

const localizeSteamAuthMessage = (message?: string | null) => {
  if (!message) return '';
  if (message === 'Install or update the server before configuring Workshop credentials.') {
    return '请先安装或更新一次服务端以构建 SteamCMD 运行环境，然后再配置 Workshop 登录。';
  }
  if (message === 'Enter the Steam account name and password used for Workshop downloads.') {
    return '请输入用于 Workshop 下载的 Steam 账户名和密码。';
  }
  if (message === 'Steam credentials are configured. Workshop downloads will log in explicitly before use.') {
    return 'Steam 凭据已配置；每次 Workshop 下载都会显式登录，不再依赖登录缓存。';
  }
  if (message === 'Steam credentials are configured and the local SteamCMD session is authorized for Workshop downloads.') {
    return 'Steam 凭据和本机授权已配置；Workshop 下载会复用已批准的 SteamCMD 会话。';
  }
  if (message === 'Verify the saved Steam credentials before downloading Workshop Mods.') {
    return '请验证已保存的 Steam 凭据后再下载 Workshop Mod。';
  }
  if (message === 'Steam Guard verification code is required') {
    return 'Steam 要求新的 Steam Guard 验证码，请输入后重新验证。';
  }
  if (message === 'Steam Mobile login confirmation is required') {
    return 'Steam 要求在手机 App 中确认本次登录。请重新提交，并立即在 Steam 手机 App 中批准新登录。';
  }
  if (message === 'invalid Steam credentials') {
    return 'Steam 拒绝了账号或密码，请重新输入。';
  }
  if (message === 'Enter the Steam account name used for the local SteamCMD session.') {
    return '请输入本机 SteamCMD 会话使用的 Steam 账户名。';
  }
  if (message.startsWith('Steam login is required')) {
    return '需要先配置 Steam 账号和密码，才能下载 Workshop Mod。';
  }
  if (message === 'Steam login operations are available only from the server host') {
    return 'Steam 登录操作只能在 PalPanel 所在的服务器主机上执行。';
  }
  if (message === 'Steam login operation was cancelled') return 'Steam 登录操作已取消。';
  if (message === 'Steam login verification timed out') return 'Steam 登录验证超时，请重新尝试。';
  if (message === 'Steam login operation failed') return 'Steam 登录操作失败，请检查账号、密码、Steam Guard 或网络连接。';
  if (message === 'Complete login in the SteamCMD window, enter quit, then verify the session.') {
    return 'SteamCMD 登录窗口正在运行。完成密码和 Steam Guard 验证后，请输入 quit 并等待窗口关闭；面板会自动检测登录缓存。';
  }
  if (message === 'Verify the cached SteamCMD session before downloading Workshop Mods.') {
    return '请验证 SteamCMD 登录缓存后再下载 Workshop MOD。';
  }
  if (message === 'Cached SteamCMD credentials are missing or expired. Open the login window and sign in again.') {
    return '未找到有效的 SteamCMD 登录缓存，或缓存已经过期。请重新打开登录窗口并完成登录，最后输入 quit 退出。';
  }
  if (message === 'SteamCMD login window is still open; complete login, enter quit, and wait for the window to close') {
    return 'SteamCMD 登录窗口仍在运行。请完成登录、输入 quit，并等待窗口关闭后再验证。';
  }
  return message;
};

const WorkshopLoginDialog: React.FC<{
  status: SteamWorkshopAuthStatus | null;
  initialError: string | null;
  canAuthenticate: boolean;
  onClose: () => void;
  onStatusChange: (status: SteamWorkshopAuthStatus) => void;
  onVerified: (status: SteamWorkshopAuthStatus) => Promise<void>;
}> = ({ status, initialError, canAuthenticate, onClose, onStatusChange, onVerified }) => {
  const [accountName, setAccountName] = useState(status?.account_name || '');
  const [password, setPassword] = useState('');
  const [steamGuardCode, setSteamGuardCode] = useState('');
  const [busy, setBusy] = useState<'login' | 'clear' | null>(null);
  const [error, setError] = useState(localizeSteamAuthMessage(initialError));
  const [notice, setNotice] = useState(initialError ? '' : localizeSteamAuthMessage(status?.message));
  const accountAvailable = Boolean(accountName.trim() || status?.account_name);
  const canUseSteamCMD = Boolean(status?.supported && status.steamcmd_installed && canAuthenticate);

  useEffect(() => {
    if (!accountName && status?.account_name) setAccountName(status.account_name);
  }, [accountName, status?.account_name]);

  const startLogin = async () => {
    if (!accountAvailable) {
      setError('请输入 Steam 账户名。');
      return;
    }
    if (!password && !status?.password_configured) {
      setError('请输入 Steam 密码。');
      return;
    }
    setBusy('login');
    setError('');
    setNotice('提交后请留意 Steam 手机 App；如果出现新登录确认，请在 90 秒内批准。');
    try {
      const next = password
        ? await modsApi.startWorkshopAuth({ accountName: accountName.trim(), password, steamGuardCode })
        : await modsApi.verifyWorkshopAuth(accountName.trim(), steamGuardCode);
      onStatusChange(next);
      if (next.logged_in) {
        setPassword('');
        setSteamGuardCode('');
        await onVerified(next);
        return;
      }
      setNotice(localizeSteamAuthMessage(next.message));
    } catch (startError) {
      if (startError instanceof ApiError && startError.code === 'steam_guard_required') {
        onStatusChange({
          ...(status || {
            supported: true,
            steamcmd_installed: true,
            credentials_secure: false,
            login_in_progress: false,
            logged_in: false,
            verification_required: true,
            password_configured: false,
            steam_guard_required: true,
          }),
          steam_guard_required: true,
          logged_in: false,
          verification_required: true,
        });
      }
      setError(localizeSteamAuthMessage(getErrorMessage(startError)));
    } finally {
      setBusy(null);
    }
  };

  const clearLogin = async () => {
    if (!window.confirm('删除 PalPanel 本机保存的 Steam 账号和密码？')) return;
    setBusy('clear');
    setError('');
    try {
      const next = await modsApi.clearWorkshopAuth();
      setAccountName('');
      setPassword('');
      setSteamGuardCode('');
      onStatusChange(next);
      setNotice('已删除本机保存的 Steam 凭据。');
    } catch (clearError) {
      setError(localizeSteamAuthMessage(getErrorMessage(clearError)));
    } finally {
      setBusy(null);
    }
  };

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-950/45 p-3 sm:p-6" role="dialog" aria-modal="true" aria-labelledby="steam-login-title">
      <div className="flex max-h-[min(720px,95dvh)] w-full max-w-lg flex-col overflow-hidden rounded-lg bg-white shadow-xl">
        <div className="flex items-start justify-between gap-3 border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <h2 id="steam-login-title" className="text-base font-bold text-slate-900">登录 Steam 以使用 Workshop</h2>
            <p className="mt-1 text-[11px] font-semibold leading-5 text-slate-500">
              Windows 与 Linux/Docker 均会保存账号和密码，并通过 Steam Guard 或手机批准建立本机授权。
            </p>
          </div>
          <button type="button" onClick={onClose} disabled={busy !== null} className="shrink-0 rounded-lg p-2 text-slate-500 hover:bg-slate-100 disabled:opacity-40" aria-label="关闭 Steam 登录">
            <X size={17} />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-5">
          <div className="flex items-start gap-3 border-b border-slate-100 pb-4">
            <ShieldCheck className="mt-0.5 shrink-0 text-emerald-600" size={17} />
            <p className="text-xs font-semibold leading-5 text-slate-600">
              安全提示：Steam 密码会以明文保存在 PalPanel 本机受限 secrets 文件中；Steam Guard 验证码仅用于本次验证，不会保存。
            </p>
          </div>

          <ol className="divide-y divide-slate-100">
            <li className="grid grid-cols-[24px_minmax(0,1fr)] gap-3 py-4">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-100 text-[11px] font-bold text-slate-600">1</span>
              <label className="grid min-w-0 gap-1.5 text-xs font-bold text-slate-700">
                Steam 账户名
                <input
                  type="text"
                  autoComplete="username"
                  spellCheck={false}
                  value={accountName}
                  onChange={(event) => setAccountName(event.target.value)}
                  disabled={busy !== null}
                  placeholder="不是个人资料昵称"
                  className="w-full rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50"
                />
              </label>
            </li>
            <li className="grid grid-cols-[24px_minmax(0,1fr)] gap-3 py-4">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-100 text-[11px] font-bold text-slate-600">2</span>
              <div className="grid min-w-0 gap-3">
                <label className="grid gap-1.5 text-xs font-bold text-slate-700">
                  Steam 密码
                  <input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} disabled={busy !== null} placeholder={status?.password_configured ? '已保存；留空则使用已保存密码' : '输入 Steam 密码'} className="w-full rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50" />
                </label>
                <label className="grid gap-1.5 text-xs font-bold text-slate-700">
                  Steam Guard 验证码（需要时填写）
                  <input type="text" inputMode="numeric" autoComplete="one-time-code" value={steamGuardCode} onChange={(event) => setSteamGuardCode(event.target.value)} disabled={busy !== null} placeholder={status?.steam_guard_required ? 'Steam 当前要求验证码' : '可留空'} className="w-full rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50" />
                </label>
              </div>
            </li>
            <li className="grid grid-cols-[24px_minmax(0,1fr)] gap-3 py-4">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-100 text-[11px] font-bold text-slate-600">3</span>
              <div className="min-w-0">
                <p className="text-xs font-bold text-slate-700">保存凭据并授权本机 SteamCMD</p>
                <p className="mt-1 text-[11px] font-semibold leading-5 text-slate-500">
                  验证成功后保存账号和密码，并复用已批准的本机 SteamCMD 会话下载；授权过期时会要求重新批准。
                </p>
                <button
                  type="button"
                  onClick={() => void startLogin()}
                  disabled={busy !== null || !canUseSteamCMD || !accountAvailable}
                  className="mt-3 inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-xs font-bold text-white hover:bg-slate-800 disabled:opacity-40"
                >
                  {busy === 'login' ? <RefreshCw size={14} className="animate-spin" /> : <ShieldCheck size={14} />}
                  {status?.password_configured && !password ? '使用已保存密码验证' : '保存并验证登录'}
                </button>
                {status?.password_configured && (
                  <button type="button" onClick={() => void clearLogin()} disabled={busy !== null} className="ml-2 mt-3 inline-flex items-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-4 py-2.5 text-xs font-bold text-rose-700 hover:bg-rose-100 disabled:opacity-40">
                    {busy === 'clear' ? <RefreshCw size={14} className="animate-spin" /> : <Trash2 size={14} />}
                    删除已保存凭据
                  </button>
                )}
              </div>
            </li>
          </ol>

          {status?.supported === false && (
            <div role="alert" className="rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-semibold leading-5 text-rose-700">当前运行平台不支持本机 SteamCMD 登录。</div>
          )}
          {status?.supported && !status.steamcmd_installed && (
            <div role="alert" className="rounded-lg border border-amber-100 bg-amber-50 px-4 py-3 text-xs font-semibold leading-5 text-amber-800">尚未安装 SteamCMD。请先在服务器安装流程中完成 SteamCMD 初始化。</div>
          )}
          {!canAuthenticate && (
            <div role="alert" className="rounded-lg border border-amber-100 bg-amber-50 px-4 py-3 text-xs font-semibold leading-5 text-amber-800">Steam 登录需由具备安全管理权限的本机管理员在面板主机上完成。</div>
          )}
          {notice && <div role="status" className="rounded-lg border border-sky-100 bg-sky-50 px-4 py-3 text-xs font-semibold leading-5 text-sky-700">{notice}</div>}
          {error && <div role="alert" className="mt-3 rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-semibold leading-5 text-rose-700">{error}</div>}
        </div>
      </div>
    </div>
    </ModalPortal>
  );
};

const ImportDialog: React.FC<{
	onClose: () => void;
	onImport: (job: Job) => Promise<void>;
	workshopAuthenticated: boolean;
	onWorkshopAuthRequired: () => void;
}> = ({ onClose, onImport, workshopAuthenticated, onWorkshopAuthRequired }) => {
	const [source, setSource] = useState('');
	const [file, setFile] = useState<File | null>(null);
	const [inspection, setInspection] = useState<ImportInspection | null>(null);
	const [candidateID, setCandidateID] = useState('');
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState('');
	const selectedCandidate = inspection?.candidates.find((candidate) => candidate.id === candidateID);

	const inspect = async () => {
		if (!file && !source.trim()) {
			setError('请选择本地 ZIP 或输入导入来源');
			return;
		}
		if (!file && isWorkshopImportSource(source) && !workshopAuthenticated) {
			setError('Workshop 导入需要先配置 Steam 登录凭据。');
			onWorkshopAuthRequired();
			return;
		}
		setBusy(true);
		setError('');
		try {
			const next = await modsApi.inspectImport({ source: source.trim(), file: file || undefined });
			setInspection(next);
			setCandidateID(next.selected_candidate_id || (next.candidates.length === 1 ? next.candidates[0].id : ''));
		} catch (inspectError) {
			setError(getErrorMessage(inspectError));
		} finally {
			setBusy(false);
		}
	};

	const chooseCandidate = async (nextID: string) => {
		if (!inspection) return;
		setCandidateID(nextID);
		const candidate = inspection.candidates.find((item) => item.id === nextID);
		if (!candidate || candidate.ready) return;
		setBusy(true);
		setError('');
		try {
			const next = await modsApi.selectImportCandidate(inspection.id, nextID);
			setInspection(next);
			setCandidateID(next.selected_candidate_id || nextID);
		} catch (selectionError) {
			setError(getErrorMessage(selectionError));
		} finally {
			setBusy(false);
		}
	};

	const startImport = async () => {
		if (!inspection || !selectedCandidate?.ready) return;
		setBusy(true);
		setError('');
		try {
			const job = await modsApi.importInspected(inspection.id, selectedCandidate.id);
			await onImport(job);
		} catch (importError) {
			setError(getErrorMessage(importError));
			setBusy(false);
		}
	};

	return (
		<ModalPortal>
		<div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/40 p-3 sm:p-6" role="dialog" aria-modal="true" aria-labelledby="mod-import-title">
			<div className="flex max-h-[min(760px,95dvh)] w-full max-w-2xl flex-col overflow-hidden rounded-lg bg-white shadow-xl">
				<div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
					<div>
						<h2 id="mod-import-title" className="text-base font-bold text-slate-900">导入 Mod</h2>
						<p className="mt-0.5 text-[11px] font-semibold text-slate-400">Workshop、GitHub Release、HTTP(S) ZIP 或本地 ZIP</p>
					</div>
					<button type="button" onClick={onClose} disabled={busy} className="rounded-lg p-2 text-slate-500 hover:bg-slate-100 disabled:opacity-40" aria-label="关闭导入">
						<X size={17} />
					</button>
				</div>

				<div className="flex flex-1 flex-col gap-5 overflow-y-auto p-5">
					{error && <div role="alert" className="rounded-lg border border-rose-100 bg-rose-50 px-4 py-3 text-xs font-semibold text-rose-700">{error}</div>}
					{!inspection && (
						<div className="grid gap-4">
							<label className="grid gap-1.5 text-xs font-bold text-slate-700">
								导入来源
								<input
									type="text"
									value={source}
									disabled={Boolean(file) || busy}
									onChange={(event) => setSource(event.target.value)}
									placeholder="Workshop ID 或 HTTP/HTTPS 地址"
									className="rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-50"
								/>
							</label>
							<div className="flex items-center gap-3 text-[10px] font-bold text-slate-300"><span className="h-px flex-1 bg-slate-100" />或<span className="h-px flex-1 bg-slate-100" /></div>
							<label className="grid gap-1.5 text-xs font-bold text-slate-700">
								本地 ZIP
								<span className="flex items-center gap-2 rounded-lg border border-slate-200 p-2.5">
									<UploadCloud size={16} className="shrink-0 text-sky-500" />
									<input
										type="file"
										accept=".zip,application/zip"
										disabled={Boolean(source.trim()) || busy}
										onChange={(event) => setFile(event.target.files?.[0] || null)}
										className="min-w-0 flex-1 text-xs font-semibold text-slate-500 file:mr-3 file:rounded-md file:border-0 file:bg-slate-900 file:px-3 file:py-1.5 file:text-[11px] file:font-bold file:text-white"
									/>
								</span>
							</label>
						</div>
					)}

					{inspection && (
						<>
							{inspection.candidates.length > 1 && (
								<label className="grid gap-1.5 text-xs font-bold text-slate-700">
									候选 ZIP
									<select value={candidateID} disabled={busy} onChange={(event) => void chooseCandidate(event.target.value)} className="rounded-lg border border-slate-200 bg-white px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none">
										<option value="">请选择</option>
										{inspection.candidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.file_name || candidate.id}{candidate.file_size ? ` (${formatBytes(candidate.file_size)})` : ''}</option>)}
									</select>
								</label>
							)}

							{selectedCandidate && (
								<div className="grid gap-4 border-y border-slate-100 py-4 sm:grid-cols-2">
									<div>
										<p className="text-[10px] font-bold text-slate-400">Mod</p>
										<p className="mt-1 text-sm font-bold text-slate-800">{selectedCandidate.name || selectedCandidate.file_name || '等待读取元数据'}</p>
										<p className="mt-1 break-all font-mono text-[10px] font-semibold text-slate-400">{selectedCandidate.package_name || '-'}</p>
									</div>
									<div>
										<p className="text-[10px] font-bold text-slate-400">安装动作</p>
										<p className="mt-1 text-sm font-bold text-slate-800">{selectedCandidate.action === 'update' ? '更新现有 Mod' : selectedCandidate.action === 'new' ? '新增 Mod（默认禁用）' : '安装时确认 PackageName'}</p>
										{selectedCandidate.version && <p className="mt-1 text-[10px] font-semibold text-slate-400">版本 {selectedCandidate.version}</p>}
									</div>
								</div>
							)}

							{selectedCandidate?.warnings && selectedCandidate.warnings.length > 0 && (
								<div className="grid gap-2 rounded-lg border border-amber-100 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
									{selectedCandidate.warnings.map((warning) => <p key={warning} className="flex items-start gap-2"><AlertTriangle size={14} className="mt-0.5 shrink-0" />{warning}</p>)}
								</div>
							)}
							<p className="text-right text-[10px] font-semibold text-slate-400">检查有效期至 {new Date(inspection.expires_at).toLocaleString()}</p>
						</>
					)}
				</div>

				<div className="flex items-center justify-between gap-3 border-t border-slate-100 px-5 py-4">
					<button type="button" onClick={inspection ? () => { setInspection(null); setCandidateID(''); setError(''); } : onClose} disabled={busy} className="rounded-lg border border-slate-200 px-4 py-2.5 text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:opacity-40">
						{inspection ? '重新选择' : '取消'}
					</button>
					{inspection ? (
						<button type="button" onClick={() => void startImport()} disabled={busy || !selectedCandidate?.ready} className="inline-flex items-center gap-2 rounded-lg bg-sky-500 px-4 py-2.5 text-xs font-bold text-white hover:bg-sky-600 disabled:opacity-40">
							{busy && <RefreshCw size={14} className="animate-spin" />}确认导入
						</button>
					) : (
						<button type="button" onClick={() => void inspect()} disabled={busy || (!file && !source.trim())} className="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-xs font-bold text-white hover:bg-slate-800 disabled:opacity-40">
							{busy && <RefreshCw size={14} className="animate-spin" />}检查
						</button>
					)}
				</div>
			</div>
		</div>
		</ModalPortal>
	);
};

const WorkshopCard: React.FC<{
  item: WorkshopItem;
  onDetail: () => void;
  onInstall: () => void;
  onInstallEnabled: () => void;
}> = ({ item, onDetail, onInstall, onInstallEnabled }) => (
  <article className="flex min-h-[360px] flex-col overflow-hidden rounded-lg border border-slate-100 bg-white shadow-sm">
    <div className="aspect-[16/9] bg-slate-100">
      {item.preview_url ? (
        <img src={item.preview_url} alt="" className="h-full w-full object-cover" loading="lazy" />
      ) : (
        <div className="flex h-full w-full items-center justify-center text-slate-300">
          <FileArchive size={36} />
        </div>
      )}
    </div>
    <div className="flex flex-1 flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="line-clamp-2 text-sm font-bold text-slate-800">{item.title}</h3>
          <p className="mt-1 font-mono text-[10px] text-slate-400">{item.id}</p>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          {item.installed && <StatusBadge status="installed" />}
          {item.enabled && <StatusBadge status="enabled" />}
          {item.update_available && <StatusBadge status="updating" customText="可更新" />}
        </div>
      </div>
      <p className="line-clamp-3 min-h-[48px] text-xs font-medium leading-4 text-slate-500">{item.summary || '-'}</p>
      <TagList tags={item.tags} />
      <div className="mt-auto flex items-center justify-between gap-3 text-[11px] font-semibold text-slate-400">
        <span>{formatBytes(item.file_size)}</span>
        <span>{formatNumber(item.subscriptions)} 订阅</span>
      </div>
      <div className="grid grid-cols-[auto_1fr_1fr] gap-2">
        <button type="button" onClick={onDetail} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="查看详情">
          <Info size={14} />
        </button>
        <button type="button" onClick={onInstall} className="rounded-lg border border-slate-200 px-3 py-2 text-xs font-bold text-slate-700 hover:bg-slate-50">
          {item.installed ? '重新安装' : '安装'}
        </button>
        <button type="button" onClick={onInstallEnabled} className="rounded-lg bg-slate-900 px-3 py-2 text-xs font-bold text-white hover:bg-slate-800">
          安装并启用
        </button>
      </div>
    </div>
  </article>
);

const ModCard: React.FC<{
  mod: ModItem;
  updateAvailable: boolean;
  onToggle: () => void;
  onDelete: () => void;
  onUpdate: () => void;
}> = ({ mod, updateAvailable, onToggle, onDelete, onUpdate }) => (
  <div className="rounded-lg border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div className="flex min-w-0 items-center gap-3">
        <ModPreview mod={mod} />
        <div className="min-w-0">
          <p className="truncate text-sm font-bold text-slate-800">{mod.name}</p>
          <p className="mt-1 truncate font-mono text-[11px] text-slate-400">{mod.package_name || mod.workshop_id || mod.id}</p>
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1">
        <StatusBadge status={mod.enabled ? 'enabled' : 'disabled'} />
        {updateAvailable && <StatusBadge status="updating" customText="可更新" />}
      </div>
    </div>
    <div className="mt-4 grid grid-cols-3 gap-2">
      {mod.source === 'workshop' ? (
        <button type="button" onClick={onUpdate} className="rounded-lg border border-slate-200 py-2 text-xs font-bold text-slate-600">
          更新
        </button>
      ) : (
        <span className="rounded-lg border border-slate-100 py-2 text-center text-xs font-bold text-slate-300">-</span>
      )}
      <button type="button" onClick={onToggle} className="rounded-lg border border-slate-200 py-2 text-xs font-bold text-slate-600">
        {mod.enabled ? '禁用' : '启用'}
      </button>
      <button type="button" onClick={onDelete} className="rounded-lg border border-rose-200 py-2 text-xs font-bold text-rose-600">
        删除
      </button>
    </div>
  </div>
);

const localSourceText: Record<LocalModFinding['source'], string> = {
  workshop: 'Workshop',
  legacy_pak: 'Pak / LogicMods',
  ue4ss: 'UE4SS',
  database: '数据库记录',
};

const localOwnershipText: Record<LocalModFinding['ownership'], string> = {
  managed: 'PalPanel 管理',
  manual: '手动放置',
};

const localStateText: Record<LocalModFinding['state'], string> = {
  present: '文件存在',
  missing_files: '文件缺失',
  unknown: '未知 Mod',
  disabled: '已禁用',
  duplicate: '重复 Mod',
  incomplete: '安装不完整',
};

const localClassificationText: Record<LocalModFinding['classifications'][number], string> = {
  managed: '已管理',
  manual: '手动安装',
  present: '文件存在',
  missing_files: '文件缺失',
  unknown: '未知',
  disabled: '已禁用',
  duplicate: '重复',
  incomplete: '不完整',
};

const localStateBadge: Record<LocalModFinding['state'], string> = {
  present: 'installed',
  missing_files: 'Error',
  unknown: 'Warning',
  disabled: 'disabled',
  duplicate: 'Warning',
  incomplete: 'Error',
};

const LocalScanMetric: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="rounded-lg border border-slate-100 bg-slate-50 px-3 py-2">
    <p className="text-[10px] font-bold text-slate-400">{label}</p>
    <p className="mt-1 break-words text-xs font-bold text-slate-700">{value}</p>
  </div>
);

const LocalFindingIdentity: React.FC<{ finding: LocalModFinding }> = ({ finding }) => (
  <div className="min-w-0">
    <p className="break-words text-xs font-bold text-slate-800">{finding.name}</p>
    <p className="mt-1 break-all font-mono text-[10px] font-semibold text-slate-400">{finding.package_name || '-'}</p>
    <div className="mt-1 flex flex-wrap gap-x-2 text-[10px] font-semibold text-slate-400">
      {finding.version && <span>版本 {finding.version}</span>}
      {finding.database_mods && finding.database_mods.length > 0 && <span>数据库记录 {finding.database_mods.length} 条</span>}
    </div>
  </div>
);

const ConfidenceBadge: React.FC<{ confidence: LocalModFinding['confidence'] }> = ({ confidence }) => {
  const text = confidence === 'high' ? '高' : confidence === 'medium' ? '中' : '低';
  const style = confidence === 'high'
    ? 'border-emerald-100 bg-emerald-50 text-emerald-700'
    : confidence === 'medium'
      ? 'border-amber-100 bg-amber-50 text-amber-700'
      : 'border-slate-100 bg-slate-50 text-slate-600';
  return <span className={`inline-flex rounded-lg border px-2.5 py-0.5 text-[11px] font-bold ${style}`}>{text}</span>;
};

const LocalFindingState: React.FC<{ finding: LocalModFinding }> = ({ finding }) => (
  <div className="grid min-w-0 gap-2">
    <div className="flex flex-wrap gap-1.5">
      <StatusBadge status={localStateBadge[finding.state]} customText={localStateText[finding.state]} />
      {finding.ignored && <StatusBadge status="Warning" customText="已忽略" />}
      {finding.state !== 'disabled' && <StatusBadge status={finding.enabled ? 'enabled' : 'disabled'} />}
      {finding.duplicate && finding.state !== 'duplicate' && <StatusBadge status="Warning" customText="重复" />}
    </div>
    {finding.classifications.length > 0 && (
      <p className="text-[10px] font-semibold leading-4 text-slate-400">
        {finding.classifications.map((classification) => localClassificationText[classification]).join(' · ')}
      </p>
    )}
    {finding.issues && finding.issues.length > 0 && (
      <ul className="grid gap-1 text-[10px] font-semibold leading-4 text-rose-600">
        {finding.issues.map((issue, index) => <li key={`${issue}-${index}`} className="break-words">{issue}</li>)}
      </ul>
    )}
  </div>
);

const LocalFindingPaths: React.FC<{ paths: string[] }> = ({ paths }) => {
  if (paths.length === 0) return <span className="text-[10px] font-semibold text-slate-400">未找到磁盘路径</span>;
  return (
    <ul className="grid gap-1.5 font-mono text-[10px] font-semibold leading-4 text-slate-500">
      {paths.map((path, index) => <li key={`${path}-${index}`} className="break-all" title={path}>{path}</li>)}
    </ul>
  );
};

const localActionLabels: Record<LocalModAction, string> = {
  import: '导入',
  repair: '修复记录',
  ignore: '忽略',
  unignore: '取消忽略',
  delete: '删除',
};

const LocalActionIcon: React.FC<{ action: LocalModAction }> = ({ action }) => {
  if (action === 'import') return <DownloadCloud size={13} />;
  if (action === 'repair') return <Wrench size={13} />;
  if (action === 'ignore') return <EyeOff size={13} />;
  if (action === 'unignore') return <Eye size={13} />;
  return <Trash2 size={13} />;
};

const LocalFindingActions: React.FC<{
  finding: LocalModFinding;
  canWrite: boolean;
  busy: string | null;
  onAction: (finding: LocalModFinding, action: LocalModAction) => void;
}> = ({ finding, canWrite, busy, onAction }) => {
  const available = finding.actions.filter((item) => item.available);
  const importCapability = finding.actions.find((item) => item.action === 'import');
  const showUnsupportedReason = !importCapability?.available && Boolean(importCapability?.reason) && (
    finding.source === 'ue4ss' || finding.source === 'legacy_pak' || finding.state === 'unknown' || finding.state === 'incomplete'
  );
  return (
    <div className="grid min-w-[9rem] gap-2">
      {canWrite && available.length > 0 && (
        <div className="flex flex-wrap justify-center gap-1.5">
          {available.map((capability) => {
            const key = `${finding.id}:${capability.action}`;
            const destructive = capability.action === 'delete';
            return (
              <button
                key={capability.action}
                type="button"
                title={capability.confirmation_required ? `${localActionLabels[capability.action]}（需要确认）` : localActionLabels[capability.action]}
                onClick={() => onAction(finding, capability.action)}
                disabled={busy !== null}
                className={`inline-flex items-center justify-center gap-1 rounded-md border px-2 py-1.5 text-[10px] font-bold disabled:opacity-50 ${
                  destructive ? 'border-rose-200 bg-rose-50 text-rose-700 hover:bg-rose-100' : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
                }`}
              >
                {busy === key ? <RefreshCw size={13} className="animate-spin" /> : <LocalActionIcon action={capability.action} />}
                {localActionLabels[capability.action]}
              </button>
            );
          })}
        </div>
      )}
      {!canWrite && <p className="text-[10px] font-semibold text-slate-400">需要 Mod 管理权限</p>}
      {showUnsupportedReason && <p className="text-left text-[10px] font-semibold leading-4 text-amber-700">{importCapability?.reason}</p>}
    </div>
  );
};

const LocalFindingCard: React.FC<{
  finding: LocalModFinding;
  canWrite: boolean;
  busy: string | null;
  onAction: (finding: LocalModFinding, action: LocalModAction) => void;
}> = ({ finding, canWrite, busy, onAction }) => (
  <article className="rounded-lg border border-slate-100 bg-white p-4 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <LocalFindingIdentity finding={finding} />
      <ConfidenceBadge confidence={finding.confidence} />
    </div>
    <div className="mt-4 grid grid-cols-2 gap-3 border-y border-slate-100 py-3">
      <div>
        <p className="text-[10px] font-bold text-slate-400">来源</p>
        <p className="mt-1 text-xs font-bold text-slate-700">{localSourceText[finding.source]}</p>
        <p className="mt-0.5 text-[10px] font-semibold text-slate-400">{localOwnershipText[finding.ownership]}</p>
      </div>
      <div>
        <p className="mb-1 text-[10px] font-bold text-slate-400">状态</p>
        <LocalFindingState finding={finding} />
      </div>
    </div>
    <div className="mt-3">
      <p className="mb-1.5 text-[10px] font-bold text-slate-400">路径</p>
      <LocalFindingPaths paths={finding.paths} />
    </div>
    <div className="mt-3 border-t border-slate-100 pt-3">
      <LocalFindingActions finding={finding} canWrite={canWrite} busy={busy} onAction={onAction} />
    </div>
  </article>
);

const ModPreview: React.FC<{ mod: ModItem }> = ({ mod }) => (
  <div className="flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-slate-100 text-slate-400">
    {mod.preview_url ? <img src={mod.preview_url} alt="" className="h-full w-full object-cover" loading="lazy" /> : <FileArchive size={16} />}
  </div>
);

const JobProgress: React.FC<{ job: Job }> = ({ job }) => (
  <div className="rounded-lg border border-slate-100 bg-white p-3">
    <div className="flex items-center justify-between gap-3 text-xs font-bold text-slate-700">
      <span className="min-w-0 truncate">{job.message || job.type}</span>
      <StatusBadge status={job.status === 'running' ? 'running_job' : job.status} />
    </div>
    <div className="mt-3 h-2 rounded-full bg-slate-100">
      <div className="h-full rounded-full bg-indigo-500" style={{ width: `${job.progress}%` }} />
    </div>
  </div>
);

const WorkshopDrawer: React.FC<{
  item: WorkshopItem;
  loading: boolean;
  translationLoading: boolean;
  translationError: string | null;
  canTranslate: boolean;
  onClose: () => void;
  onTranslate: (force: boolean) => Promise<void>;
  onInstall: () => void;
  onInstallEnabled: () => void;
}> = ({ item, loading, translationLoading, translationError, canTranslate, onClose, onTranslate, onInstall, onInstallEnabled }) => {
  const [descriptionView, setDescriptionView] = useState<'original' | 'zh-CN'>(item.translation ? 'zh-CN' : 'original');

  useEffect(() => {
    if (item.translation) setDescriptionView('zh-CN');
  }, [item.translation]);

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-50 flex justify-end bg-slate-950/40">
    <aside className="flex h-full w-full max-w-xl flex-col overflow-y-auto bg-white shadow-xl">
      <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
        <div className="min-w-0">
          <h2 className="truncate text-base font-bold text-slate-900">{item.title}</h2>
          <p className="font-mono text-[11px] font-semibold text-slate-400">{item.id}</p>
        </div>
        <button type="button" onClick={onClose} className="rounded-lg border border-slate-200 p-2 text-slate-500 hover:bg-slate-50" aria-label="关闭">
          <X size={16} />
        </button>
      </div>
      {item.preview_url && <img src={item.preview_url} alt="" className="aspect-[16/9] w-full object-cover" />}
      <div className="flex flex-col gap-5 p-5">
        {loading && (
          <div className="text-xs font-semibold text-slate-400">
            <RefreshCw className="mr-2 inline animate-spin text-sky-500" size={14} />
            正在读取详情...
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          {item.installed && <StatusBadge status="installed" />}
          {item.enabled && <StatusBadge status="enabled" />}
          {item.update_available && <StatusBadge status="updating" customText="可更新" />}
        </div>
        <div>
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            {item.translation ? (
              <div className="inline-flex rounded-lg border border-slate-200 bg-slate-50 p-1">
                <button type="button" onClick={() => setDescriptionView('original')} className={`rounded-md px-3 py-1.5 text-[11px] font-bold ${descriptionView === 'original' ? 'bg-white text-slate-800 shadow-sm' : 'text-slate-400'}`}>原文</button>
                <button type="button" onClick={() => setDescriptionView('zh-CN')} className={`rounded-md px-3 py-1.5 text-[11px] font-bold ${descriptionView === 'zh-CN' ? 'bg-white text-slate-800 shadow-sm' : 'text-slate-400'}`}>中文</button>
              </div>
            ) : <span />}
            {canTranslate && (
              <button
                type="button"
                onClick={() => void onTranslate(Boolean(item.translation))}
                disabled={translationLoading || loading}
                className="inline-flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs font-bold text-emerald-700 hover:bg-emerald-100 disabled:opacity-40"
              >
                {translationLoading ? <RefreshCw className="animate-spin" size={14} /> : <Languages size={14} />}
                {item.translation ? '重新翻译' : '翻译为中文'}
              </button>
            )}
          </div>
          <p className="whitespace-pre-wrap break-words text-sm leading-6 text-slate-600">
            {descriptionView === 'zh-CN' && item.translation ? item.translation.text : item.summary || '-'}
          </p>
          {item.translation && descriptionView === 'zh-CN' && (
            <p className="mt-3 text-[10px] font-semibold text-slate-400">
              {item.translation.model || '未知模型'} · {item.translation.generated_at ? new Date(item.translation.generated_at).toLocaleString('zh-CN') : '-'}{item.translation.cached ? ' · 缓存' : ''}
            </p>
          )}
          {translationError && <p className="mt-3 rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-[11px] font-semibold text-rose-700">{translationError}</p>}
        </div>
        <TagList tags={item.tags} />
        <div className="grid grid-cols-2 gap-3 text-xs font-semibold text-slate-500">
          <InfoTile label="大小" value={formatBytes(item.file_size)} />
          <InfoTile label="订阅" value={formatNumber(item.subscriptions)} />
          <InfoTile label="创建" value={formatSteamTime(item.time_created)} />
          <InfoTile label="更新" value={formatSteamTime(item.time_updated)} />
        </div>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-[auto_1fr_1fr]">
          <a
            href={item.steam_url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center justify-center gap-2 rounded-lg border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50"
          >
            <ExternalLink size={14} />
            Steam
          </a>
          <button type="button" onClick={onInstall} className="inline-flex items-center justify-center gap-2 rounded-lg border border-slate-200 px-4 py-2 text-xs font-bold text-slate-700 hover:bg-slate-50">
            <PackageCheck size={14} />
            {item.installed ? '重新安装' : '安装'}
          </button>
          <button type="button" onClick={onInstallEnabled} className="inline-flex items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-xs font-bold text-white hover:bg-slate-800">
            <CheckCircle2 size={14} />
            安装并启用
          </button>
        </div>
      </div>
    </aside>
  </div>
  </ModalPortal>
  );
};

const TagList: React.FC<{ tags: string[] }> = ({ tags }) => {
  if (tags.length === 0) {
    return <div className="min-h-[24px]" />;
  }
  return (
    <div className="flex min-h-[24px] flex-wrap gap-1.5">
      {tags.slice(0, 4).map((tag) => (
        <span key={tag} className="rounded-md border border-slate-100 bg-slate-50 px-2 py-0.5 text-[10px] font-bold text-slate-500">
          {tag}
        </span>
      ))}
    </div>
  );
};

const InfoTile: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="rounded-lg border border-slate-100 bg-slate-50 px-3 py-2">
    <p className="text-[10px] font-bold uppercase text-slate-400">{label}</p>
    <p className="mt-1 truncate text-xs font-bold text-slate-700">{value}</p>
  </div>
);

const formatBytes = (value?: number) => {
  if (!value || value <= 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
};

const formatNumber = (value?: number) => {
  if (!value || value <= 0) return '-';
  return new Intl.NumberFormat('zh-CN', { notation: value >= 10000 ? 'compact' : 'standard' }).format(value);
};

const formatSteamTime = (value?: number) => {
  if (!value || value <= 0) return '-';
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(new Date(value * 1000));
};

const formatLocalScanTime = (value: string) => {
  if (!value) return '-';
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(parsed);
};
