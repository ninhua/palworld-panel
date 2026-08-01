import React, { useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle,
  Braces,
  Clock3,
  FileCode2,
  FolderCog,
  ExternalLink,
  LoaderCircle,
  MapPinned,
  RotateCcw,
  Save,
  Search,
  ShieldCheck,
} from 'lucide-react';
import { getErrorMessage } from '../../api/client';
import { modConfigurationsApi } from '../../api/modConfigurations';
import { securityApi } from '../../api/security';
import type {
  LocalModFinding,
  ModConfigBackup,
  ModConfigDocument,
  ModConfigFile,
  ModConfigurationAdapter,
  ModConfigurationField,
  ModItem,
} from '../../types';
import { useI18n, type TranslationKey } from '../../i18n';
import PalZonesEditorWorkspace from './palzones/PalZonesEditorWorkspace';

interface Props {
  mods: ModItem[];
  localFindings: LocalModFinding[];
  canWrite: boolean;
  canReloadPalDefender: boolean;
}

type Selection =
  | { kind: 'adapter'; id: string; name: string }
  | { kind: 'mod'; id: string; name: string };

const adapterTranslations: Record<string, { name: TranslationKey; description: TranslationKey }> = {
  paldefender: { name: 'modAdapter.paldefender.name', description: 'modAdapter.paldefender.description' },
  ue4ss: { name: 'modAdapter.ue4ss.name', description: 'modAdapter.ue4ss.description' },
  palschema: { name: 'modAdapter.palschema.name', description: 'modAdapter.palschema.description' },
  'extended-base-range': { name: 'modAdapter.extended-base-range.name', description: 'modAdapter.extended-base-range.description' },
  'quality-of-life': { name: 'modAdapter.quality-of-life.name', description: 'modAdapter.quality-of-life.description' },
};

export const ModConfigWorkspace: React.FC<Props> = ({ mods, localFindings, canWrite, canReloadPalDefender }) => {
  const { locale, t } = useI18n();
  const [adapters, setAdapters] = useState<ModConfigurationAdapter[]>([]);
  const [selection, setSelection] = useState<Selection | null>(null);
  const [files, setFiles] = useState<ModConfigFile[]>([]);
  const [selectedFile, setSelectedFile] = useState<ModConfigFile | null>(null);
  const [document, setDocument] = useState<ModConfigDocument | null>(null);
  const [content, setContent] = useState('');
  const [backups, setBackups] = useState<ModConfigBackup[]>([]);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const modOptions = useMemo(() => {
    const seen = new Set<string>();
    const values: Array<{ id: string; name: string; detail: string }> = [];
    for (const mod of mods) {
      seen.add(mod.id);
      values.push({ id: mod.id, name: mod.name, detail: mod.workshop_id || mod.package_name || mod.id });
    }
    for (const finding of localFindings) {
      if (seen.has(finding.id)) continue;
      values.push({ id: finding.id, name: finding.name, detail: `${finding.source} · ${t('modConfig.localDetected')}` });
    }
    return values.sort((left, right) => left.name.localeCompare(right.name, locale));
  }, [localFindings, locale, mods, t]);

  const selectedAdapter = selection?.kind === 'adapter'
    ? adapters.find((item) => item.id === selection.id)
    : undefined;

  useEffect(() => {
    let active = true;
    setLoading(true);
    modConfigurationsApi.listAdapters()
      .then((items) => {
        if (!active) return;
        setAdapters(items);
        const first = items.find((item) => item.available);
        if (first) setSelection({ kind: 'adapter', id: first.id, name: first.name });
      })
      .catch((reason) => active && setError(getErrorMessage(reason)))
      .finally(() => active && setLoading(false));
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!selection) {
      setFiles([]);
      setSelectedFile(null);
      setDocument(null);
      return;
    }
    setError(null);
    setNotice(null);
    setDocument(null);
    setContent('');
    if (selection.kind === 'adapter') {
      const adapter = adapters.find((item) => item.id === selection.id);
      const nextFiles = adapter?.files || [];
      setFiles(nextFiles);
      setSelectedFile(nextFiles[0] || null);
      return;
    }
    setLoading(true);
    modConfigurationsApi.listFiles(selection.id)
      .then((items) => {
        setFiles(items);
        setSelectedFile(items[0] || null);
      })
      .catch((reason) => setError(getErrorMessage(reason)))
      .finally(() => setLoading(false));
  }, [adapters, selection]);

  useEffect(() => {
    if (!selection || !selectedFile) return;
    let active = true;
    setLoading(true);
    setError(null);
    const request = selection.kind === 'adapter'
      ? modConfigurationsApi.getAdapter(selection.id, selectedFile.id)
      : modConfigurationsApi.getFile(selection.id, selectedFile.id);
    request.then((next) => {
      if (!active) return;
      setDocument(next);
      setContent(next.content);
      return (selection.kind === 'adapter'
        ? modConfigurationsApi.listAdapterBackups(selection.id, selectedFile.id)
        : modConfigurationsApi.listFileBackups(selection.id, selectedFile.id));
    }).then((items) => {
      if (active && items) setBackups(items);
    }).catch((reason) => active && setError(getErrorMessage(reason)))
      .finally(() => active && setLoading(false));
    return () => { active = false; };
  }, [selectedFile, selection]);

  const filteredFiles = useMemo(() => {
    const keyword = search.trim().toLowerCase();
    if (!keyword) return files;
    return files.filter((file) => file.path.toLowerCase().includes(keyword));
  }, [files, search]);

  const diff = useMemo(() => document && content !== document.content
    ? buildLineDiff(document.content, content)
    : [], [content, document]);

  const runAdapterAction = async (action: 'initialize' | 'enable') => {
    if (!selectedAdapter || !canWrite) return;
    setSaving(true);
    setError(null);
    try {
      const result = await modConfigurationsApi.runAction(selectedAdapter.id, action);
      setAdapters((current) => current.map((item) => item.id === result.adapter.id ? result.adapter : item));
      if (result.document) {
        setFiles([result.document.file]);
        setSelectedFile(result.document.file);
        setDocument(result.document);
        setContent(result.document.content);
      }
      setNotice(result.restart_required ? '配置已写入，等待安全重启服务器后生效。' : '配置状态未发生变化。');
    } catch (reason) {
      setError(getErrorMessage(reason));
    } finally {
      setSaving(false);
    }
  };

  const saveContent = async (nextContent: string) => {
    if (!selection || !document || !canWrite) return;
    const executable = document.file.executable;
    if (executable && !window.confirm(t('modConfig.luaConfirm'))) return;
    setSaving(true);
    setError(null);
    try {
      const next = selection.kind === 'adapter'
        ? await modConfigurationsApi.saveAdapter(selection.id, document.file.id, nextContent, document.file.revision, executable)
        : await modConfigurationsApi.saveFile(selection.id, document.file.id, nextContent, document.file.revision, executable);
      setDocument(next);
      setContent(next.content);
      const adapter = selection.kind === 'adapter' ? adapters.find((item) => item.id === selection.id) : undefined;
      setNotice(adapter?.reload_behavior === 'online_reload' ? t('modConfig.savedReload') : t('modConfig.savedRestart'));
      setBackups(selection.kind === 'adapter'
        ? await modConfigurationsApi.listAdapterBackups(selection.id, next.file.id)
        : await modConfigurationsApi.listFileBackups(selection.id, next.file.id));
    } catch (reason) {
      setError(getErrorMessage(reason));
    } finally {
      setSaving(false);
    }
  };

  const restore = async (backup: ModConfigBackup) => {
    if (!selection || !document || !canWrite || !window.confirm(t('modConfig.restoreConfirm'))) return;
    setSaving(true);
    setError(null);
    try {
      const next = selection.kind === 'adapter'
        ? await modConfigurationsApi.restoreAdapter(selection.id, document.file.id, backup.id, document.file.revision)
        : await modConfigurationsApi.restoreFile(selection.id, document.file.id, backup.id, document.file.revision);
      setDocument(next);
      setContent(next.content);
      setNotice(t('modConfig.restored'));
    } catch (reason) {
      setError(getErrorMessage(reason));
    } finally {
      setSaving(false);
    }
  };

  const reloadPalDefender = async () => {
    setSaving(true);
    setError(null);
    try {
      const result = await securityApi.reloadConfig();
      setNotice(result.reloaded ? t('modConfig.reloadSuccess') : t('modConfig.reloadUnknown'));
    } catch (reason) {
      setError(getErrorMessage(reason));
    } finally {
      setSaving(false);
    }
  };

  const updateField = (field: ModConfigurationField, value: unknown) => {
    if (!document) return;
    const next = updateStructuredContent(content, document.format, field.path, value);
    if (next != null) setContent(next);
  };

  return (
    <section className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm">
      <div className="border-b border-slate-100 bg-gradient-to-r from-slate-950 via-slate-900 to-sky-950 px-5 py-5 text-white sm:px-7">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2 text-sm font-black"><FolderCog size={18} className="text-sky-300" />{t('modConfig.title')}</div>
            <p className="mt-1 text-xs text-slate-300">{t('modConfig.description')}</p>
          </div>
          <div className="flex items-center gap-2 rounded-full bg-white/10 px-3 py-1.5 text-[11px] font-bold text-slate-200">
            <ShieldCheck size={13} className="text-emerald-300" />{t('modConfig.safety')}
          </div>
        </div>
      </div>

      <div className="grid min-h-[620px] lg:grid-cols-[280px_1fr]">
        <aside className="border-b border-slate-200 bg-slate-50/80 p-4 lg:border-b-0 lg:border-r">
          <p className="mb-2 text-[10px] font-black uppercase tracking-[0.18em] text-slate-400">{t('modConfig.adapters')}</p>
          <div className="space-y-2">
            {adapters.map((adapter) => {
              const translated = adapterTranslations[adapter.id];
              const adapterName = translated ? t(translated.name) : adapter.name;
              const adapterDescription = translated ? t(translated.description) : adapter.description;
              return (
                <button
                  key={adapter.id}
                  type="button"
                  disabled={!adapter.available}
                  onClick={() => setSelection({ kind: 'adapter', id: adapter.id, name: adapterName })}
                  className={`w-full rounded-xl border p-3 text-left transition ${selection?.kind === 'adapter' && selection.id === adapter.id ? 'border-sky-300 bg-sky-50 shadow-sm' : 'border-slate-200 bg-white hover:border-slate-300'} disabled:cursor-not-allowed disabled:opacity-45`}
                >
                  <div className="flex items-center justify-between gap-2"><span className="text-xs font-black text-slate-800">{adapterName}</span><Braces size={14} className="text-sky-500" /></div>
                  <p className="mt-1 line-clamp-2 text-[10px] leading-4 text-slate-500">{adapterDescription}</p>
                  <p className="mt-2 text-[10px] font-bold text-slate-400">{adapter.available ? t('modConfig.files', { count: adapter.files.length }) : t('modConfig.notDetected')}</p>
                </button>
              );
            })}
          </div>

          <p className="mb-2 mt-5 text-[10px] font-black uppercase tracking-[0.18em] text-slate-400">{t('modConfig.otherMods')}</p>
          <select
            aria-label={t('modConfig.selectMod')}
            value={selection?.kind === 'mod' ? selection.id : ''}
            onChange={(event) => {
              const target = modOptions.find((item) => item.id === event.target.value);
              if (target) setSelection({ kind: 'mod', id: target.id, name: target.name });
            }}
            className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-bold text-slate-700 outline-none focus:border-sky-400"
          >
            <option value="">{t('modConfig.selectModPlaceholder')}</option>
            {modOptions.map((mod) => <option key={mod.id} value={mod.id}>{mod.name} · {mod.detail}</option>)}
          </select>

          {files.length > 0 && (
            <div className="mt-4">
              <label className="relative block">
                <Search size={13} className="absolute left-3 top-2.5 text-slate-400" />
                <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t('modConfig.searchFiles')} className="w-full rounded-lg border border-slate-200 bg-white py-2 pl-8 pr-3 text-xs outline-none focus:border-sky-400" />
              </label>
              <div className="mt-2 max-h-64 space-y-1 overflow-y-auto">
                {filteredFiles.map((file) => (
                  <button key={file.id} type="button" onClick={() => setSelectedFile(file)} className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[11px] font-bold ${selectedFile?.id === file.id ? 'bg-slate-900 text-white' : 'text-slate-600 hover:bg-slate-100'}`}>
                    <FileCode2 size={13} className={file.executable ? 'text-amber-400' : 'text-sky-500'} />
                    <span className="min-w-0 truncate">{file.path}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </aside>

        <main className="min-w-0 p-4 sm:p-6">
          {error && <div className="mb-4 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs font-semibold text-rose-700">{error}</div>}
          {notice && <div className="mb-4 rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-xs font-semibold text-emerald-700">{notice}</div>}
          {loading && <div className="flex h-64 items-center justify-center gap-2 text-xs font-bold text-slate-400"><LoaderCircle size={16} className="animate-spin" />{t('modConfig.reading')}</div>}
          {!loading && selection && files.length === 0 && selectedAdapter?.id === 'palzones' && (
            <PalZonesSetup adapter={selectedAdapter} canWrite={canWrite} saving={saving} onInitialize={() => runAdapterAction('initialize')} />
          )}
          {!loading && selection && files.length === 0 && selectedAdapter?.id !== 'palzones' && <EmptyConfig name={selection.name} />}
          {!loading && document && selection?.kind === 'adapter' && selection.id === 'palzones' && (
            <PalZonesEditorWorkspace document={document} canWrite={canWrite} saving={saving} onSave={saveContent} />
          )}
          {!loading && document && !(selection?.kind === 'adapter' && selection.id === 'palzones') && (
            <div className="space-y-5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h2 className="text-base font-black text-slate-900">{selection?.name}</h2>
                  <p className="mt-1 font-mono text-[11px] text-slate-500">{document.file.path}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {selection?.kind === 'adapter' && selection.id === 'paldefender' && (
                    <>
                      <button type="button" disabled={!canWrite || saving} onClick={() => setContent(applyPalDefenderPreset(content))} className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 text-xs font-black text-slate-600 hover:bg-slate-50 disabled:opacity-40">{t('modConfig.balancePreset')}</button>
                      {canReloadPalDefender && <button type="button" disabled={saving || content !== document.content} onClick={reloadPalDefender} className="rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2.5 text-xs font-black text-emerald-700 hover:bg-emerald-100 disabled:opacity-40">{t('modConfig.reloadOnline')}</button>}
                    </>
                  )}
                  <button type="button" disabled={!canWrite || saving || content === document.content} onClick={() => void saveContent(content)} className="inline-flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2.5 text-xs font-black text-white hover:bg-sky-600 disabled:cursor-not-allowed disabled:opacity-40">
                    {saving ? <LoaderCircle size={14} className="animate-spin" /> : <Save size={14} />}{t('modConfig.saveChanges')}
                  </button>
                </div>
              </div>

              {document.file.executable && <div className="flex gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-xs text-amber-800"><AlertTriangle size={18} className="shrink-0" /><div><p className="font-black">{t('modConfig.luaTitle')}</p><p className="mt-1 leading-5">{document.file.risk}</p></div></div>}

              {(document.fields?.length || 0) > 0 && selection?.kind === 'adapter' && (
                <div>
                  <h3 className="mb-3 text-xs font-black text-slate-700">{t('modConfig.visualFields')}</h3>
                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                    {document.fields?.map((field) => <FieldEditor key={field.path} field={field} content={content} format={document.format} disabled={!canWrite} onChange={(value) => updateField(field, value)} />)}
                  </div>
                </div>
              )}

              <div>
                <div className="mb-2 flex items-center justify-between"><h3 className="text-xs font-black text-slate-700">{t('modConfig.rawFile')}</h3><span className="text-[10px] font-bold text-slate-400">1 MiB · {document.format.toUpperCase()}</span></div>
                <textarea aria-label={t('modConfig.rawFileLabel')} value={content} readOnly={!canWrite} onChange={(event) => setContent(event.target.value)} spellCheck={false} className="min-h-[320px] w-full resize-y rounded-xl border border-slate-800 bg-slate-950 p-4 font-mono text-xs leading-6 text-slate-100 outline-none focus:border-sky-500 read-only:opacity-75" />
              </div>

              {diff.length > 0 && (
                <div>
                  <h3 className="mb-2 text-xs font-black text-slate-700">{t('modConfig.diff')}</h3>
                  <div className="max-h-56 overflow-auto rounded-xl border border-slate-800 bg-slate-950 p-3 font-mono text-[11px] leading-5">
                    {diff.map((line, index) => <div key={`${line.kind}-${index}`} className={line.kind === '+' ? 'text-emerald-300' : 'text-rose-300'}>{line.kind} {line.text || ' '}</div>)}
                  </div>
                </div>
              )}

              <div>
                <h3 className="mb-3 flex items-center gap-2 text-xs font-black text-slate-700"><Clock3 size={14} />{t('modConfig.backups')}</h3>
                {backups.length === 0 ? <p className="text-xs text-slate-400">{t('modConfig.firstBackup')}</p> : (
                  <div className="space-y-2">{backups.slice(0, 12).map((backup) => (
                    <div key={backup.id} className="flex items-center justify-between gap-3 rounded-xl border border-slate-200 px-3 py-2.5">
                      <div><p className="text-[11px] font-bold text-slate-700">{new Date(backup.created_at).toLocaleString(locale)}</p><p className="mt-0.5 font-mono text-[9px] text-slate-400">{backup.revision.slice(0, 12)} · {formatBytes(backup.size)}</p></div>
                      <button type="button" disabled={!canWrite || saving} onClick={() => restore(backup)} className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 px-2.5 py-1.5 text-[10px] font-black text-slate-600 hover:bg-slate-50 disabled:opacity-40"><RotateCcw size={12} />{t('modConfig.restore')}</button>
                    </div>
                  ))}</div>
                )}
              </div>
            </div>
          )}
        </main>
      </div>
    </section>
  );
};

const EmptyConfig: React.FC<{ name: string }> = ({ name }) => {
  const { t } = useI18n();
  return (
  <div className="flex h-72 flex-col items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-center">
    <FileCode2 size={28} className="text-slate-300" />
    <p className="mt-3 text-sm font-black text-slate-700">{t('modConfig.noConfig')}</p>
    <p className="mt-1 max-w-sm text-xs leading-5 text-slate-400">{t('modConfig.noConfigDescription', { name })}</p>
  </div>
  );
};

const PalZonesSetup: React.FC<{
  adapter: ModConfigurationAdapter;
  canWrite: boolean;
  saving: boolean;
  onInitialize: () => void;
}> = ({ adapter, canWrite, saving, onInitialize }) => {
  const action = adapter.actions.find((item) => item.id === 'initialize');
  return (
    <div className={'flex min-h-72 flex-col items-center justify-center border border-dashed border-slate-200 bg-slate-50 px-6 text-center'}>
      <span className={'grid h-11 w-11 place-items-center rounded-lg bg-sky-100 text-sky-700'}><MapPinned size={22} /></span>
      <h2 className={'mt-4 text-sm font-black text-slate-800'}>尚未创建区域配置</h2>
      <p className={'mt-2 max-w-md text-xs leading-5 text-slate-500'}>PalZones 已安装。创建操作只会在 Mod 目录中新增合法的 Config/zones.json，不会覆盖已有文件。</p>
      <div className={'mt-4 flex flex-wrap items-center justify-center gap-2'}>
        <button type={'button'} disabled={!canWrite || saving || !action?.available} onClick={onInitialize} className={'rounded-lg bg-sky-600 px-4 py-2.5 text-xs font-black text-white hover:bg-sky-700 disabled:cursor-not-allowed disabled:opacity-40'}>创建默认配置</button>
        {adapter.reference_urls?.editor && <a href={adapter.reference_urls.editor} target={'_blank'} rel={'noreferrer'} className={'inline-flex items-center gap-1.5 px-3 py-2 text-xs font-bold text-slate-600'}>参考原版编辑器 <ExternalLink size={13} /></a>}
      </div>
    </div>
  );
};

const FieldEditor: React.FC<{
  field: ModConfigurationField;
  content: string;
  format: string;
  disabled: boolean;
  onChange: (value: unknown) => void;
}> = ({ field, content, format, disabled, onChange }) => {
  const current = currentFieldValue(content, format, field) ?? field.value;
  return (
    <label className="rounded-xl border border-slate-200 bg-slate-50/70 p-3">
      <span className="block truncate text-[10px] font-black text-slate-500" title={field.path}>{field.label}</span>
      <span className="mt-0.5 block truncate font-mono text-[9px] text-slate-400" title={field.path}>{field.path}</span>
      {field.type === 'boolean' ? (
        <input type="checkbox" checked={Boolean(current)} disabled={disabled} onChange={(event) => onChange(event.target.checked)} className="mt-3 h-4 w-4 accent-sky-500" />
      ) : (
        <input
          type={field.type === 'string' ? 'text' : 'number'}
          value={String(current ?? '')}
          min={field.min}
          max={field.max}
          disabled={disabled}
          onChange={(event) => onChange(field.type === 'string' ? event.target.value : Number(event.target.value))}
          className="mt-2 w-full rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-xs font-semibold text-slate-700 outline-none focus:border-sky-400"
        />
      )}
    </label>
  );
};

const currentFieldValue = (content: string, format: string, field: ModConfigurationField): unknown => {
  if (format !== 'json') return field.value;
  try {
    let value: unknown = JSON.parse(content);
    for (const segment of field.path.split('.')) value = value && typeof value === 'object' ? (value as Record<string, unknown>)[segment] : undefined;
    return value;
  } catch {
    return field.value;
  }
};

const updateStructuredContent = (content: string, format: string, path: string, value: unknown): string | null => {
  if (format === 'json') {
    try {
      const root = JSON.parse(content) as Record<string, unknown>;
      const segments = path.split('.');
      let cursor = root;
      for (const segment of segments.slice(0, -1)) {
        const next = cursor[segment];
        if (!next || typeof next !== 'object' || Array.isArray(next)) return null;
        cursor = next as Record<string, unknown>;
      }
      cursor[segments[segments.length - 1]] = value;
      return `${JSON.stringify(root, null, 2)}\n`;
    } catch {
      return null;
    }
  }
  const key = path.split('.').at(-1) || path;
  if (format === 'ini' || format === 'cfg' || format === 'toml') {
    const encoded = typeof value === 'string' ? value : String(value);
    const expression = new RegExp(`^(\\s*${escapeRegExp(key)}\\s*=\\s*).*$`, 'm');
    return expression.test(content) ? content.replace(expression, `$1${encoded}`) : null;
  }
  if (format === 'lua') {
    const expression = new RegExp(`^(\\s*(?:local\\s+)?${escapeRegExp(key)}\\s*=\\s*)[-+]?\\d+(?:\\.\\d+)?`, 'm');
    return expression.test(content) ? content.replace(expression, `$1${String(value)}`) : null;
  }
  return null;
};

const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

const formatBytes = (value: number) => value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KiB`;

const palDefenderBalancedPreset = {
  version: '1.0.0',
  MOTD: ['Welcome to {ServerName}', 'PvP: {IsPvP} | Death penalty: {DeathPenalty}'],
  exitServerOnStartupFailure: false,
  preventAdminPasswordInChat: true,
  shouldWarnCheaters: true,
  shouldWarnCheatersReason: true,
  shouldKickCheaters: true,
  shouldBanCheaters: false,
  shouldIPBanCheaters: false,
  logChat: true,
  logRCON: true,
  logPlayerUID: true,
  logPlayerIP: true,
  logPlayerDeaths: true,
  logPlayerLogins: true,
  logPlayerBuildings: true,
  logPlayerSummons: true,
  logPlayerCaptures: true,
  logCraftings: true,
  logTechUnlocks: true,
  useAdminWhitelist: false,
  adminAutoLogin: false,
  adminIPs: [],
  bannedChatWords: [],
  bannedNames: [],
  announceConnections: true,
  dontAnnounceAdminConnections: true,
  announcePunishments: true,
  useWhitelist: false,
  whitelistMessage: 'This server uses a whitelist.',
  steamidProtection: true,
  blockTowerBossCapture: true,
  disableIllegalItemProtection: false,
  disableButchering: false,
  disableRenaming: false,
  disablePalRenaming: false,
  doActionUponIllegalPalStats: true,
  palStatsMaxRank: -1,
  bannedTechnologies: [],
};

const applyPalDefenderPreset = (content: string) => {
  try {
    const current = JSON.parse(content) as Record<string, unknown>;
    return `${JSON.stringify({ ...current, ...palDefenderBalancedPreset }, null, 2)}\n`;
  } catch {
    return `${JSON.stringify(palDefenderBalancedPreset, null, 2)}\n`;
  }
};

const buildLineDiff = (before: string, after: string) => {
  const left = before.split('\n');
  const right = after.split('\n');
  const output: Array<{ kind: '-' | '+'; text: string }> = [];
  const count = Math.max(left.length, right.length);
  for (let index = 0; index < count && output.length < 120; index += 1) {
    if (left[index] === right[index]) continue;
    if (left[index] !== undefined) output.push({ kind: '-', text: left[index] });
    if (right[index] !== undefined) output.push({ kind: '+', text: right[index] });
  }
  return output;
};
