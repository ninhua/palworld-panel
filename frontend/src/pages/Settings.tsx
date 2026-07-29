import React, { useEffect, useMemo, useState } from 'react';
import { AlertTriangle, CheckCircle2, Copy, Info, KeyRound, Languages, Network, Plus, RefreshCw, Save, Shield, Trash2 } from 'lucide-react';
import { getErrorMessage } from '../api/client';
import { authApi } from '../api/auth';
import { aiTranslationApi } from '../api/aiTranslation';
import { serverApi } from '../api/server';
import { settingsApi } from '../api/settings';
import { networkProxyApi } from '../api/networkProxy';
import { useServerStore } from '../store/useServerStore';
import type { AITranslationConfig, AITranslationConfigUpdate, DevelopmentKey, FieldSchema, NetworkProxyConfig, NetworkProxyConfigUpdate, PalworldConfigDraft, PalworldFormatIssue, PalworldSettings, ServerVersionInfo, ValidationIssue } from '../types';
import { useI18n } from '../i18n';
import { LanguageSwitcher } from '../components/ui/LanguageSwitcher';
import { ConfigRevisionHistory } from '../components/ConfigRevisionHistory';

const groupLabels: Record<string, string> = {
  server_management: '服务器管理',
  performance: '性能',
  game_balance: '游戏平衡',
  features: '功能开关',
  pvp: 'PvP 对战',
  technology: '科技限制',
};

const coerceInitialValue = (field: FieldSchema, value: unknown) => {
  const raw = value ?? field.default;
  if (raw === undefined) return undefined;
  if (field.type === 'bool') {
    return String(raw).toLowerCase() === 'true';
  }
  if (field.type === 'int' || field.type === 'float') {
    const number = Number(raw);
    return Number.isFinite(number) ? number : undefined;
  }
  return String(raw);
};

export const Settings: React.FC = () => {
  const { t } = useI18n();
  const { triggerRefresh, refreshAuthentication, session } = useServerStore();
  const [fields, setFields] = useState<FieldSchema[]>([]);
  const [draft, setDraft] = useState<PalworldSettings>({});
  const [path, setPath] = useState('');
  const [version, setVersion] = useState('1.0.0');
  const [versionInfo, setVersionInfo] = useState<ServerVersionInfo | null>(null);
  const [originalKeys, setOriginalKeys] = useState<Set<string>>(new Set());
  const [dirtyKeys, setDirtyKeys] = useState<Set<string>>(new Set());
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [pendingRestart, setPendingRestart] = useState(false);
  const [configDraft, setConfigDraft] = useState<PalworldConfigDraft | null>(null);
  const [formatIssues, setFormatIssues] = useState<PalworldFormatIssue[]>([]);
  const [clearSecrets, setClearSecrets] = useState<Set<string>>(new Set());
  const [activeGroup, setActiveGroup] = useState('server_management');
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState<string | null>(null);
  const [aiConfig, setAIConfig] = useState<AITranslationConfig | null>(null);
  const [aiConfigLoading, setAIConfigLoading] = useState(true);
  const [aiBaseURL, setAIBaseURL] = useState('');
  const [aiModel, setAIModel] = useState('');
  const [aiAPIKey, setAIAPIKey] = useState('');
  const [clearAIAPIKey, setClearAIAPIKey] = useState(false);
  const [aiTimeoutSeconds, setAITimeoutSeconds] = useState(90);
  const [aiProxyURL, setAIProxyURL] = useState('');
  const [clearAIProxy, setClearAIProxy] = useState(false);
  const [aiCustomHeaders, setAICustomHeaders] = useState('');
  const [clearAICustomHeaders, setClearAICustomHeaders] = useState(false);
  const [aiBusy, setAIBusy] = useState(false);
  const [networkConfig, setNetworkConfig] = useState<NetworkProxyConfig | null>(null);
  const [networkConfigLoading, setNetworkConfigLoading] = useState(true);
  const [installProxyEnabled, setInstallProxyEnabled] = useState(false);
  const [installProxyURL, setInstallProxyURL] = useState('');
  const [clearInstallProxy, setClearInstallProxy] = useState(false);
  const [communityProxyEnabled, setCommunityProxyEnabled] = useState(false);
  const [communityProxyURL, setCommunityProxyURL] = useState('');
  const [clearCommunityProxy, setClearCommunityProxy] = useState(false);
  const [networkBusy, setNetworkBusy] = useState(false);
  const [developmentKeys, setDevelopmentKeys] = useState<DevelopmentKey[]>([]);
  const [developmentKeyName, setDevelopmentKeyName] = useState('本机自动化');
  const [revealedDevelopmentKey, setRevealedDevelopmentKey] = useState('');
  const [developmentKeysBusy, setDevelopmentKeysBusy] = useState(false);
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [passwordBusy, setPasswordBusy] = useState(false);
  const canConfigureAI = Boolean(session?.permissions.includes('ai:config'));
  const canConfigureNetwork = Boolean(session?.permissions.includes('config:write'));

  const load = async () => {
    setLoading(true);
    try {
      const [schema, status, serverVersion] = await Promise.all([
        settingsApi.getSchema(),
        serverApi.getStatus(),
        serverApi.getVersion(),
      ]);
      const config = status.config_exists
        ? await settingsApi.getSettings()
        : { settings: {}, path: status.settings_path || '', pending_restart: status.pending_restart, issues: [] };
      const nextDraft: PalworldSettings = {};
      schema.fields.forEach((field) => {
        nextDraft[field.key] = coerceInitialValue(field, config.settings[field.key]);
      });
      Object.entries(config.settings).forEach(([key, value]) => {
        if (!(key in nextDraft)) nextDraft[key] = value;
      });
      setFields(schema.fields);
      setVersion(schema.version);
      setVersionInfo(serverVersion);
      setDraft(nextDraft);
      const configuredKeys = new Set(Object.keys(config.settings));
      if (config.secret_state?.admin_password.configured) configuredKeys.add('AdminPassword');
      if (config.secret_state?.server_password.configured) configuredKeys.add('ServerPassword');
      setOriginalKeys(configuredKeys);
	  setClearSecrets(new Set());
      setDirtyKeys(new Set());
      setPath(config.path);
      setIssues(config.issues || []);
      setPendingRestart(config.pending_restart);
      setConfigDraft(config.draft || null);
      setFormatIssues(config.format_issues || []);
      setActiveGroup(schema.fields[0]?.group || 'server_management');
    } catch (error) {
      setFields([]);
      setDraft({});
      setIssues([]);
      setMessage(getErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  useEffect(() => {
    let active = true;
    setNetworkConfigLoading(true);
    void networkProxyApi.getConfig()
      .then((config) => {
        if (!active) return;
        setNetworkConfig(config);
        setInstallProxyEnabled(config.install.enabled);
        setInstallProxyURL('');
        setClearInstallProxy(false);
        setCommunityProxyEnabled(config.community.enabled);
        setCommunityProxyURL('');
        setClearCommunityProxy(false);
      })
      .catch((error) => {
        if (active) setMessage(getErrorMessage(error));
      })
      .finally(() => {
        if (active) setNetworkConfigLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!canConfigureAI) return;
    let active = true;
    setAIConfigLoading(true);
    void aiTranslationApi.getConfig()
      .then((config) => {
        if (!active) return;
        setAIConfig(config);
        setAIBaseURL(config.base_url);
        setAIModel(config.model);
        setAIAPIKey('');
        setClearAIAPIKey(false);
        setAITimeoutSeconds(config.timeout_seconds || 90);
        setAIProxyURL('');
        setClearAIProxy(false);
        setAICustomHeaders('');
        setClearAICustomHeaders(false);
      })
      .catch((error) => {
        if (active) setMessage(getErrorMessage(error));
      })
      .finally(() => {
        if (active) setAIConfigLoading(false);
      });
    return () => {
      active = false;
    };
  }, [canConfigureAI]);

  useEffect(() => {
    if (!session?.permissions.includes('security:write')) return;
    void authApi.listKeys()
      .then((keys) => {
        setDevelopmentKeys((current) => {
          const currentIDs = new Set(current.map((key) => key.id));
          return [...current, ...keys.filter((key) => !currentIDs.has(key.id))];
        });
      })
      .catch((error) => setMessage(getErrorMessage(error)));
  }, [session]);

  const groups = useMemo(() => {
    const ids = Array.from(new Set(fields.map((field) => field.group)));
    return ids.map((id) => ({ id, label: groupLabels[id] || id, fields: fields.filter((field) => field.group === id) }));
  }, [fields]);

  const activeFields = groups.find((group) => group.id === activeGroup)?.fields || [];
  const errorCount = issues.filter((issue) => issue.severity === 'error').length;
  const warningCount = issues.filter((issue) => issue.severity === 'warning').length;

  const updateField = (key: string, value: string | number | boolean) => {
    setDraft((prev) => ({ ...prev, [key]: value }));
    setDirtyKeys((prev) => new Set(prev).add(key));
  };

  const submission = () =>
    Object.fromEntries(
      Object.entries(draft).filter(
        ([key, value]) => value !== undefined && (originalKeys.has(key) || dirtyKeys.has(key)),
      ),
    ) as PalworldSettings;

  const validate = async () => {
    const result = await settingsApi.validateSettings(submission());
    setIssues(result.issues);
    setMessage(result.valid ? '配置校验通过' : '配置存在错误，请修正后再保存');
    return result.valid;
  };

  const save = async () => {
    const valid = await validate();
    if (!valid) return;
    try {
      const updates = submission();
      const saved = clearSecrets.size > 0
        ? await settingsApi.updateSettings(updates, Array.from(clearSecrets))
        : await settingsApi.updateSettings(updates);
      setPendingRestart(saved.pending_restart);
      setConfigDraft(saved.draft || null);
      setFormatIssues(saved.format_issues || []);
      setIssues(saved.issues || []);
      setOriginalKeys(new Set(Object.keys(saved.settings)));
      setDirtyKeys(new Set());
      setClearSecrets(new Set());
      setMessage('配置草稿已保存，请确认后应用');
      triggerRefresh();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const createDevelopmentKey = async () => {
    if (!developmentKeyName.trim()) return;
    setDevelopmentKeysBusy(true);
    try {
      const key = await authApi.createKey(developmentKeyName.trim());
      setRevealedDevelopmentKey(key.token || '');
      setDevelopmentKeys((current) => [key, ...current]);
      setMessage('开发密钥已创建，请立即保存；关闭后无法再次查看完整值');
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setDevelopmentKeysBusy(false);
    }
  };

  const revokeDevelopmentKey = async (id: string) => {
    setDevelopmentKeysBusy(true);
    try {
      await authApi.revokeKey(id);
      setDevelopmentKeys((current) => current.map((key) => key.id === id ? { ...key, revoked_at: new Date().toISOString() } : key));
      setMessage('开发密钥已撤销');
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setDevelopmentKeysBusy(false);
    }
  };

  const aiUpdate = (): AITranslationConfigUpdate => {
    let customHeaders: Record<string, string> | undefined;
    if (aiCustomHeaders.trim()) {
      const decoded: unknown = JSON.parse(aiCustomHeaders);
      if (!decoded || Array.isArray(decoded) || typeof decoded !== 'object') {
        throw new Error('自定义请求头必须是 JSON 对象');
      }
      customHeaders = {};
      for (const [name, value] of Object.entries(decoded)) {
        if (typeof value !== 'string') {
          throw new Error(`请求头 ${name} 的值必须是字符串`);
        }
        customHeaders[name] = value;
      }
    }
    return {
      base_url: aiBaseURL.trim(),
      model: aiModel.trim(),
      timeout_seconds: aiTimeoutSeconds,
      ...(aiAPIKey.trim() ? { api_key: aiAPIKey.trim() } : {}),
      ...(clearAIAPIKey ? { clear_api_key: true } : {}),
      ...(aiProxyURL.trim() ? { proxy_url: aiProxyURL.trim() } : {}),
      ...(clearAIProxy ? { clear_proxy: true } : {}),
      ...(customHeaders ? { custom_headers: customHeaders } : {}),
      ...(clearAICustomHeaders ? { clear_custom_headers: true } : {}),
    };
  };

  const saveAIConfig = async () => {
    setAIBusy(true);
    try {
      const saved = await aiTranslationApi.updateConfig(aiUpdate());
      setAIConfig(saved);
      setAIBaseURL(saved.base_url);
      setAIModel(saved.model);
      setAIAPIKey('');
      setClearAIAPIKey(false);
      setAITimeoutSeconds(saved.timeout_seconds || 90);
      setAIProxyURL('');
      setClearAIProxy(false);
      setAICustomHeaders('');
      setClearAICustomHeaders(false);
      setMessage(saved.configured ? 'AI 翻译配置已保存' : 'AI 翻译配置已保存，但尚未完整配置');
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setAIBusy(false);
    }
  };

  const testAIConfig = async () => {
    setAIBusy(true);
    try {
      await aiTranslationApi.testConfig(aiUpdate());
      setMessage('AI 翻译连接测试通过');
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setAIBusy(false);
    }
  };

  const changePassword = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!currentPassword) {
      setMessage('请输入当前管理员密码');
      return;
    }
    if (newPassword.length < 12 || newPassword.length > 128) {
      setMessage('新密码长度必须为 12–128 个字符');
      return;
    }
    if (newPassword !== confirmPassword) {
      setMessage('两次输入的新密码不一致');
      return;
    }
    setPasswordBusy(true);
    try {
      await authApi.changePassword(currentPassword, newPassword);
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      await refreshAuthentication();
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setPasswordBusy(false);
    }
  };

  const applyConfigDraft = async () => {
    if (!configDraft) return;
    try {
      const job = await settingsApi.applySettings(configDraft.id);
      setConfigDraft({ ...configDraft, status: 'applying', applied_job_id: job.id });
      setMessage('配置应用任务已提交');
      triggerRefresh();
    } catch (error) {
      setMessage(getErrorMessage(error));
    }
  };

  const saveNetworkConfig = async () => {
    setNetworkBusy(true);
    try {
      const update: NetworkProxyConfigUpdate = {
        install_enabled: clearInstallProxy ? false : installProxyEnabled,
        community_enabled: clearCommunityProxy ? false : communityProxyEnabled,
        ...(installProxyURL.trim() ? { install_proxy_url: installProxyURL.trim() } : {}),
        ...(clearInstallProxy ? { clear_install_proxy: true } : {}),
        ...(communityProxyURL.trim() ? { community_proxy_url: communityProxyURL.trim() } : {}),
        ...(clearCommunityProxy ? { clear_community_proxy: true } : {}),
      };
      const saved = await networkProxyApi.updateConfig(update);
      setNetworkConfig(saved);
      setInstallProxyEnabled(saved.install.enabled);
      setInstallProxyURL('');
      setClearInstallProxy(false);
      setCommunityProxyEnabled(saved.community.enabled);
      setCommunityProxyURL('');
      setClearCommunityProxy(false);
      setMessage(t('settings.proxySaved'));
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setNetworkBusy(false);
    }
  };

  const testNetworkProxy = async (scope: 'install' | 'community') => {
    setNetworkBusy(true);
    try {
      const result = await networkProxyApi.test(scope);
      setMessage(result.ok
        ? t('settings.proxyTestPassed', { latency: result.latency_ms, status: result.http_status })
        : `${result.message || '代理测试失败'}${result.failure_stage ? ` (${result.failure_stage})` : ''}`);
    } catch (error) {
      setMessage(getErrorMessage(error));
    } finally {
      setNetworkBusy(false);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center p-12 text-xs font-semibold text-slate-400">
        <RefreshCw className="mr-2 animate-spin text-sky-500" size={16} />
        正在读取服务器配置...
      </div>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <section className="flex flex-col gap-4 rounded-2xl border border-slate-100 bg-white p-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-sm font-bold text-slate-800">{t('settings.languageTitle')}</h2>
          <p className="mt-1 text-xs font-medium text-slate-400">{t('settings.languageDescription')}</p>
        </div>
        <LanguageSwitcher buttons />
      </section>

      <section className="rounded-2xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-sky-50 p-2.5 text-sky-600"><Network size={19} /></div>
            <div>
              <h2 className="text-sm font-bold text-slate-800">{t('settings.proxyTitle')}</h2>
              <p className="mt-1 max-w-3xl text-xs font-medium leading-relaxed text-slate-400">{t('settings.proxyDescription')}</p>
            </div>
          </div>
          {canConfigureNetwork && (
            <button type="button" onClick={() => void saveNetworkConfig()} disabled={networkBusy || networkConfigLoading} className="inline-flex items-center justify-center gap-2 rounded-lg bg-sky-600 px-4 py-2 text-xs font-bold text-white hover:bg-sky-700 disabled:opacity-40">
              <Save size={14} />{t('settings.proxySave')}
            </button>
          )}
        </div>

        {networkConfigLoading ? (
          <div className="mt-5 flex items-center gap-2 text-xs font-semibold text-slate-400"><RefreshCw className="animate-spin" size={14} />{t('settings.proxyLoading')}</div>
        ) : (
          <div className="mt-5 grid grid-cols-1 gap-4 lg:grid-cols-2">
            {([
              {
                scope: 'install' as const,
                title: t('settings.proxyInstallTitle'),
                description: t('settings.proxyInstallDescription'),
                endpoint: networkConfig?.install,
                enabled: installProxyEnabled,
                setEnabled: setInstallProxyEnabled,
                value: installProxyURL,
                setValue: setInstallProxyURL,
                clear: clearInstallProxy,
                setClear: setClearInstallProxy,
              },
              {
                scope: 'community' as const,
                title: t('settings.proxyCommunityTitle'),
                description: t('settings.proxyCommunityDescription'),
                endpoint: networkConfig?.community,
                enabled: communityProxyEnabled,
                setEnabled: setCommunityProxyEnabled,
                value: communityProxyURL,
                setValue: setCommunityProxyURL,
                clear: clearCommunityProxy,
                setClear: setClearCommunityProxy,
              },
            ]).map((item) => (
              <div key={item.scope} className="rounded-2xl border border-slate-100 bg-slate-50/60 p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="text-xs font-bold text-slate-700">{item.title}</h3>
                    <p className="mt-1 text-[11px] font-medium leading-relaxed text-slate-400">{item.description}</p>
                  </div>
                  <label className="inline-flex shrink-0 items-center gap-2 text-[11px] font-bold text-slate-600">
                    <input type="checkbox" checked={item.enabled} disabled={!canConfigureNetwork || item.clear} onChange={(event) => item.setEnabled(event.target.checked)} className="h-4 w-4 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                    {t('settings.proxyEnabled')}
                  </label>
                </div>
                <div className="mt-4 rounded-xl border border-slate-100 bg-white px-3 py-2.5 text-[10px] font-semibold text-slate-500">
                  {item.endpoint?.configured
                    ? t('settings.proxyConfiguredAs', { url: item.endpoint.url || item.endpoint.scheme || '' })
                    : t('settings.proxyNotConfigured')}
                  {item.endpoint?.authentication_configured ? ` · ${t('settings.proxyAuthStored')}` : ''}
                  {item.endpoint?.source === 'environment' ? ` · ${t('settings.proxyEnvironmentSource')}` : ''}
                </div>
                <label className="mt-4 flex flex-col gap-2 text-[11px] font-bold text-slate-600">
                  {t('settings.proxyURL')}
                  <input
                    type="password"
                    value={item.value}
                    disabled={!canConfigureNetwork || item.clear}
                    onChange={(event) => { item.setValue(event.target.value); if (event.target.value) item.setClear(false); }}
                    placeholder={item.endpoint?.configured ? t('settings.proxyKeepExisting') : 'socks5://127.0.0.1:10808'}
                    className="rounded-lg border border-slate-200 bg-white px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none disabled:bg-slate-100"
                  />
                </label>
                <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                  <label className="inline-flex items-center gap-2 text-[11px] font-semibold text-rose-600">
                    <input type="checkbox" checked={item.clear} disabled={!canConfigureNetwork || !item.endpoint?.configured} onChange={(event) => { item.setClear(event.target.checked); if (event.target.checked) item.setValue(''); }} className="h-4 w-4 rounded border-slate-300 text-rose-500 focus:ring-rose-500" />
                    {t('settings.proxyClear')}
                  </label>
                  {canConfigureNetwork && item.endpoint?.enabled && (
                    <button type="button" onClick={() => void testNetworkProxy(item.scope)} disabled={networkBusy} className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 py-1.5 text-[11px] font-bold text-slate-600 hover:bg-slate-100 disabled:opacity-40">
                      <RefreshCw className={networkBusy ? 'animate-spin' : ''} size={12} />{t('settings.proxyTest')}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="mt-4 flex items-start gap-2 rounded-xl border border-amber-100 bg-amber-50 px-4 py-3 text-[11px] font-medium leading-relaxed text-amber-700">
          <AlertTriangle className="mt-0.5 shrink-0" size={14} />
          <span>{t('settings.proxySecurityNote')}</span>
        </div>
      </section>

      {message && (
        <div className="flex items-center gap-2.5 rounded-2xl border border-sky-100 bg-sky-50 px-5 py-3.5 text-xs font-semibold text-sky-700">
          <Info size={16} />
          {message}
        </div>
      )}

      {pendingRestart && (
        <div className="rounded-2xl border border-amber-100 bg-amber-50 px-5 py-4">
          <div className="flex items-center gap-2 text-xs font-bold text-amber-800">
            <AlertTriangle size={16} />
            配置已写入，等待重启生效
          </div>
          <p className="mt-1 text-[11px] font-medium leading-relaxed text-amber-700">
            PalWorldSettings.ini 修改后需要重启 Palworld 服务端。启动后的状态接口会清除待重启标记。
          </p>
        </div>
      )}

      {formatIssues.some((issue) => issue.code === 'string_not_quoted') && (
        <div className="flex items-center gap-2 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs font-semibold text-amber-800">
          <AlertTriangle size={15} />
          密码字符串格式不正确，将在应用草稿时自动修复。
        </div>
      )}

      <div className="grid grid-cols-1 gap-6 xl:grid-cols-[260px_minmax(0,1fr)]">
        <aside className="rounded-3xl border border-slate-100 bg-white p-4 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
          <div className="mb-4 rounded-2xl bg-slate-50 p-4">
            <p className="text-[11px] font-semibold text-slate-400">配置规范</p>
            <p className="mt-1 text-sm font-bold text-slate-800">配置规范 {version}</p>
            <dl className="mt-3 grid gap-1.5 text-[10px] font-semibold text-slate-500">
              <div className="flex justify-between gap-3"><dt>游戏版本</dt><dd>{versionInfo?.game_version || '离线未知'}</dd></div>
              <div className="flex justify-between gap-3"><dt>当前 Build</dt><dd>{versionInfo?.current_build_id || '未知'}</dd></div>
              <div className="flex justify-between gap-3"><dt>最新 Build</dt><dd>{versionInfo?.latest_build_id || '未检查'}</dd></div>
              <div className="flex justify-between gap-3">
                <dt>兼容状态</dt>
                <dd>{versionInfo?.compatible === true ? '兼容' : versionInfo?.compatible === false ? '不匹配' : '待运行确认'}</dd>
              </div>
            </dl>
            <p className="mt-2 break-all text-[10px] font-medium leading-relaxed text-slate-400">
              {path || '配置文件尚未初始化'}
            </p>
          </div>

          {versionInfo?.compatibility_warnings && versionInfo.compatibility_warnings.length > 0 && (
            <div className="mb-4 rounded-2xl border border-amber-100 bg-amber-50 p-3 text-[10px] font-semibold leading-relaxed text-amber-800">
              {versionInfo.compatibility_warnings.join(' / ')}
            </div>
          )}

          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:flex xl:flex-col">
            {groups.length > 0 ? (
              groups.map((group) => (
                <button
                  type="button"
                  key={group.id}
                  onClick={() => setActiveGroup(group.id)}
                  className={`shrink-0 rounded-xl px-4 py-3 text-left text-xs font-bold transition-all ${
                    activeGroup === group.id
                      ? 'bg-sky-100 text-sky-800 ring-1 ring-sky-200'
                      : 'bg-slate-50 text-slate-500 hover:bg-slate-100 hover:text-slate-800'
                  }`}
                >
                  {group.label}
                </button>
              ))
            ) : (
              <p className="rounded-2xl bg-slate-50 p-4 text-xs font-semibold text-slate-400">
                后端未返回配置 schema。请先完成开服初始化，或检查 `/config/palworld/schema`。
              </p>
            )}
          </div>
        </aside>

        <section className="min-w-0 rounded-3xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)]">
          <div className="mb-5 flex flex-col gap-3 border-b border-slate-100 pb-5 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <h3 className="text-[16px] font-bold text-slate-800">{groupLabels[activeGroup] || activeGroup}</h3>
              <p className="mt-1 text-xs font-medium text-slate-400">
                字段来自后端 `/config/palworld/schema`，保存前会执行后端校验。
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={validate}
                className="flex items-center gap-2 rounded-xl border border-slate-200 px-4 py-2 text-xs font-semibold text-slate-600 hover:bg-slate-50"
              >
                <CheckCircle2 size={14} />
                校验
              </button>
              <button
                type="button"
                onClick={save}
                className="flex items-center gap-2 rounded-xl bg-sky-500 px-4 py-2 text-xs font-semibold text-white hover:bg-sky-600"
              >
                <Save size={14} />
                保存
              </button>
              {configDraft && (configDraft.status === 'draft' || configDraft.status === 'failed') && (
                <button
                  type="button"
                  onClick={() => void applyConfigDraft()}
                  className="flex items-center gap-2 rounded-xl bg-emerald-600 px-4 py-2 text-xs font-semibold text-white hover:bg-emerald-700"
                >
                  <CheckCircle2 size={14} />
                  应用草稿
                </button>
              )}
            </div>
          </div>

          {issues.length > 0 && (
            <div className="mb-5 rounded-2xl border border-slate-100 bg-slate-50 p-4">
              <div className="mb-2 flex items-center gap-2 text-xs font-bold text-slate-700">
                <Shield size={15} className={errorCount > 0 ? 'text-rose-500' : 'text-amber-500'} />
                校验结果：{errorCount} 个错误，{warningCount} 个警告
              </div>
              <div className="grid gap-2">
                {issues.map((issue, index) => (
                  <p
                    key={index}
                    className={`rounded-xl px-3 py-2 text-[11px] font-semibold ${
                      issue.severity === 'error'
                        ? 'bg-rose-50 text-rose-700'
                        : issue.severity === 'warning'
                          ? 'bg-amber-50 text-amber-700'
                          : 'bg-sky-50 text-sky-700'
                    }`}
                  >
                    {issue.field ? `${issue.field}: ` : ''}
                    {issue.message}
                  </p>
                ))}
              </div>
            </div>
          )}

          {activeFields.length > 0 ? (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {activeFields.map((field) => (
                <FieldControl
                  key={field.key}
                  field={field}
                  value={draft[field.key]}
                  isSet={originalKeys.has(field.key) || dirtyKeys.has(field.key)}
                  onChange={(value) => updateField(field.key, value)}
                  secretConfigured={field.key === 'AdminPassword'
                    ? originalKeys.has(field.key)
                    : field.key === 'ServerPassword' ? originalKeys.has(field.key) : false}
                  clearSecret={clearSecrets.has(field.key)}
                  onClearSecret={(clear) => setClearSecrets((current) => {
                    const next = new Set(current);
                    if (clear) next.add(field.key); else next.delete(field.key);
                    return next;
                  })}
                />
              ))}
            </div>
          ) : (
            <div className="rounded-2xl border border-dashed border-slate-200 bg-slate-50/70 px-4 py-12 text-center text-xs font-semibold text-slate-400">
              暂无可编辑配置字段
            </div>
          )}

          <div className="sticky bottom-20 mt-6 flex justify-end border-t border-slate-100 bg-white/95 pt-4 backdrop-blur lg:bottom-0">
            <button
              type="button"
              onClick={save}
              className="flex w-full items-center justify-center gap-2.5 rounded-2xl bg-sky-500 px-6 py-3.5 text-xs font-bold text-white hover:bg-sky-600 sm:w-auto"
            >
              <Save size={15} />
              保存全部参数
            </button>
          </div>
        </section>
      </div>

      {canConfigureNetwork && (
        <ConfigRevisionHistory
          canRestore={Boolean(session?.permissions.includes('server:control'))}
          onDraftCreated={setConfigDraft}
          onApplied={() => triggerRefresh()}
        />
      )}

      {session && session.name !== 'local' && (
        <section className="rounded-2xl border border-slate-100 bg-white p-5 shadow-[0_2px_12px_-3px_rgba(15,23,42,0.02)] sm:p-6">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-violet-50 p-2.5 text-violet-600"><KeyRound size={19} /></div>
            <div>
              <h2 className="text-sm font-bold text-slate-800">修改管理员密码</h2>
              <p className="mt-1 text-xs font-medium leading-relaxed text-slate-400">需要验证当前密码。修改成功后，全部登录会话和开发密钥都会立即失效，并返回登录页。</p>
            </div>
          </div>
          <form onSubmit={(event) => void changePassword(event)} className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-3">
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              当前密码
              <input type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} autoComplete="current-password" aria-label="当前管理员密码" className="rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-violet-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              新密码
              <input type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} minLength={12} maxLength={128} autoComplete="new-password" aria-label="新管理员密码" className="rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-violet-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              确认新密码
              <input type="password" value={confirmPassword} onChange={(event) => setConfirmPassword(event.target.value)} minLength={12} maxLength={128} autoComplete="new-password" aria-label="确认新管理员密码" className="rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-violet-500 focus:outline-none" />
            </label>
            <div className="flex flex-col gap-3 md:col-span-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-2 text-[11px] font-semibold leading-relaxed text-amber-700"><AlertTriangle className="mt-0.5 shrink-0" size={14} />请提前保存仍需使用的开发密钥配置；修改密码后旧密钥无法恢复。</div>
              <button type="submit" disabled={passwordBusy || !currentPassword || !newPassword || !confirmPassword} className="inline-flex shrink-0 items-center justify-center gap-2 rounded-lg bg-violet-600 px-4 py-2.5 text-xs font-bold text-white hover:bg-violet-700 disabled:opacity-40">
                <KeyRound size={14} />{passwordBusy ? '正在修改...' : '修改密码并重新登录'}
              </button>
            </div>
          </form>
        </section>
      )}

      {session?.permissions.includes('security:write') && (
        <section className="border-y border-slate-100 bg-white py-5 sm:py-6">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <div className="flex items-center gap-2 text-[15px] font-bold text-slate-800"><KeyRound size={17} className="text-sky-500" />开发密钥</div>
              <p className="mt-1 text-[11px] font-semibold text-slate-400">用于脚本和 API 客户端，拥有管理员权限。</p>
            </div>
            <div className="flex w-full gap-2 sm:w-auto">
              <input
                type="text"
                value={developmentKeyName}
                onChange={(event) => setDevelopmentKeyName(event.target.value)}
                maxLength={64}
                aria-label="开发密钥名称"
                className="min-w-0 flex-1 rounded-lg border border-slate-200 px-3 py-2.5 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none sm:w-52"
              />
              <button type="button" onClick={() => void createDevelopmentKey()} disabled={developmentKeysBusy || !developmentKeyName.trim()} className="inline-flex shrink-0 items-center gap-2 rounded-lg bg-slate-900 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-40">
                <Plus size={14} />创建
              </button>
            </div>
          </div>

          {revealedDevelopmentKey && (
            <div className="mt-4 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3">
              <code className="min-w-0 flex-1 break-all text-xs font-semibold text-amber-900">{revealedDevelopmentKey}</code>
              <button type="button" title="复制开发密钥" aria-label="复制开发密钥" onClick={() => void navigator.clipboard.writeText(revealedDevelopmentKey)} className="shrink-0 rounded-lg p-2 text-amber-700 hover:bg-amber-100">
                <Copy size={15} />
              </button>
            </div>
          )}

          <div className="mt-4 divide-y divide-slate-100 border-y border-slate-100">
            {developmentKeys.length === 0 ? (
              <p className="py-5 text-center text-xs font-semibold text-slate-400">尚未创建开发密钥</p>
            ) : developmentKeys.map((key) => (
              <div key={key.id} className="flex items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="truncate text-xs font-bold text-slate-700">{key.name}</p>
                  <p className="mt-0.5 font-mono text-[10px] font-semibold text-slate-400">{key.prefix}... · {key.revoked_at ? '已撤销' : '有效'}</p>
                </div>
                {!key.revoked_at && (
                  <button type="button" title="撤销开发密钥" aria-label={`撤销 ${key.name}`} onClick={() => void revokeDevelopmentKey(key.id)} disabled={developmentKeysBusy} className="shrink-0 rounded-lg p-2 text-rose-500 hover:bg-rose-50 disabled:opacity-40">
                    <Trash2 size={15} />
                  </button>
                )}
              </div>
            ))}
          </div>
        </section>
      )}

      {canConfigureAI && aiConfigLoading && (
        <section className="flex min-h-32 items-center justify-center rounded-xl border border-slate-200 bg-white p-5 text-sm font-semibold text-slate-500 sm:p-6">
          <RefreshCw className="mr-2 animate-spin text-sky-500" size={15} />
          正在读取 AI 翻译配置...
        </section>
      )}

      {canConfigureAI && !aiConfigLoading && (
        <section className="rounded-lg border border-slate-100 bg-white p-5 sm:p-6">
          <div className="flex flex-col gap-3 border-b border-slate-100 pb-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-3">
              <div className="rounded-lg bg-emerald-50 p-2 text-emerald-600"><Languages size={18} /></div>
              <div>
                <h3 className="text-[15px] font-bold text-slate-800">AI 翻译</h3>
                <p className="mt-0.5 text-[11px] font-semibold text-slate-400">{aiConfig?.configured ? '已配置' : '未完整配置'}</p>
              </div>
            </div>
            <div className="flex gap-2">
              <button type="button" onClick={() => void testAIConfig()} disabled={aiBusy} className="inline-flex items-center gap-2 rounded-lg border border-slate-200 px-4 py-2 text-xs font-bold text-slate-600 hover:bg-slate-50 disabled:opacity-40">
                <RefreshCw className={aiBusy ? 'animate-spin' : ''} size={14} />连接测试
              </button>
              <button type="button" onClick={() => void saveAIConfig()} disabled={aiBusy} className="inline-flex items-center gap-2 rounded-lg bg-emerald-600 px-4 py-2 text-xs font-bold text-white hover:bg-emerald-700 disabled:opacity-40">
                <Save size={14} />保存 AI 配置
              </button>
            </div>
          </div>

          <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600 md:col-span-2">
              OpenAI-compatible Base URL
              <input type="url" value={aiBaseURL} onChange={(event) => setAIBaseURL(event.target.value)} placeholder="http://ai.local:8000/v1 或 https://api.example.com/v1" className="rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
              <span className="text-[10px] font-medium text-slate-400">支持 HTTP 和 HTTPS；跨公网使用 HTTP 时请求内容和 API Key 不会被加密。</span>
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              Model
              <input type="text" value={aiModel} onChange={(event) => setAIModel(event.target.value)} placeholder="model-name" className="rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              <span className="flex items-center gap-2"><KeyRound size={13} />API Key {aiConfig?.api_key_present ? '（已保存）' : ''}</span>
              <input type="password" value={aiAPIKey} onChange={(event) => { setAIAPIKey(event.target.value); if (event.target.value) setClearAIAPIKey(false); }} placeholder={aiConfig?.api_key_present ? '留空以保留现有密钥' : '输入 API Key'} className="rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              请求超时（秒）
              <input type="number" min={1} max={600} value={aiTimeoutSeconds} onChange={(event) => setAITimeoutSeconds(Number(event.target.value))} className="rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600">
              Proxy URL {aiConfig?.proxy_configured ? `（已保存：${aiConfig.proxy_url || '私密代理'}）` : ''}
              <input type="password" value={aiProxyURL} onChange={(event) => { setAIProxyURL(event.target.value); if (event.target.value) setClearAIProxy(false); }} placeholder={aiConfig?.proxy_configured ? '留空以保留现有代理' : 'socks5://127.0.0.1:10808'} className="rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
            </label>
            <label className="flex flex-col gap-2 text-xs font-bold text-slate-600 md:col-span-2">
              自定义请求头 {aiConfig?.custom_header_names.length ? `（已保存：${aiConfig.custom_header_names.join(', ')}）` : ''}
              <textarea rows={3} value={aiCustomHeaders} onChange={(event) => { setAICustomHeaders(event.target.value); if (event.target.value) setClearAICustomHeaders(false); }} placeholder={'{"X-Tenant-ID":"tenant-a"}'} spellCheck={false} className="resize-y rounded-lg border border-slate-200 px-3 py-2.5 font-mono text-xs font-semibold text-slate-700 focus:border-emerald-500 focus:outline-none" />
            </label>
          </div>
          <div className="mt-4 flex flex-wrap gap-x-6 gap-y-3">
            <label className="inline-flex items-center gap-2 text-xs font-semibold text-rose-600">
              <input type="checkbox" checked={clearAIAPIKey} onChange={(event) => { setClearAIAPIKey(event.target.checked); if (event.target.checked) setAIAPIKey(''); }} className="h-4 w-4 rounded border-slate-300 text-rose-500 focus:ring-rose-500" />
              删除已保存的 API Key
            </label>
            <label className="inline-flex items-center gap-2 text-xs font-semibold text-rose-600">
              <input type="checkbox" checked={clearAIProxy} onChange={(event) => { setClearAIProxy(event.target.checked); if (event.target.checked) setAIProxyURL(''); }} className="h-4 w-4 rounded border-slate-300 text-rose-500 focus:ring-rose-500" />
              删除已保存的代理
            </label>
            <label className="inline-flex items-center gap-2 text-xs font-semibold text-rose-600">
              <input type="checkbox" checked={clearAICustomHeaders} onChange={(event) => { setClearAICustomHeaders(event.target.checked); if (event.target.checked) setAICustomHeaders(''); }} className="h-4 w-4 rounded border-slate-300 text-rose-500 focus:ring-rose-500" />
              删除已保存的请求头
            </label>
          </div>
        </section>
      )}
    </div>
  );
};

const FieldControl: React.FC<{
  field: FieldSchema;
  value: string | number | boolean | undefined;
  isSet: boolean;
  onChange: (value: string | number | boolean) => void;
  secretConfigured?: boolean;
  clearSecret?: boolean;
  onClearSecret?: (clear: boolean) => void;
}> = ({ field, value, isSet, onChange, secretConfigured, clearSecret, onClearSecret }) => {
  const commonLabel = (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <span className="break-all text-xs font-bold text-slate-700">{field.label || field.key}</span>
        <p className="mt-0.5 break-all font-mono text-[9px] font-semibold leading-relaxed text-slate-400">
          {field.key}
        </p>
        <p className="mt-0.5 text-[10px] font-medium leading-relaxed text-slate-400">{field.description}</p>
      </div>
      {field.requires_restart && (
        <span className="shrink-0 rounded-lg bg-amber-50 px-2 py-0.5 text-[9px] font-bold text-amber-600">需重启</span>
      )}
    </div>
  );

  if (!isSet) {
    const initialValue =
      coerceInitialValue(field, field.default) ??
      (field.type === 'bool' ? false : field.type === 'int' || field.type === 'float' ? 0 : '');
    return (
      <div className="flex min-h-[104px] flex-col justify-between rounded-2xl border border-dashed border-slate-200 bg-slate-50/70 p-4">
        {commonLabel}
        <div className="mt-3 flex items-center justify-between gap-3">
          <span className="text-[10px] font-semibold text-slate-400">
            {field.default === undefined ? '当前未设置' : `当前未设置，服务端默认 ${field.default}`}
          </span>
          <button
            type="button"
            onClick={() => onChange(initialValue)}
            className="flex shrink-0 items-center gap-1 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-[10px] font-bold text-slate-600 hover:bg-slate-100"
          >
            <Plus size={12} />
            设置
          </button>
        </div>
      </div>
    );
  }

  if (field.type === 'bool') {
    return (
      <label className="flex min-h-[104px] flex-col justify-between rounded-2xl border border-slate-100 bg-slate-50/70 p-4">
        {commonLabel}
        <div className="mt-3 flex items-center gap-2 text-xs font-semibold text-slate-600">
          <input
            type="checkbox"
            checked={Boolean(value)}
            onChange={(event) => onChange(event.target.checked)}
            className="h-4 w-4 rounded border-slate-300 text-sky-500 focus:ring-sky-500"
          />
          {value ? '开启' : '关闭'}
        </div>
      </label>
    );
  }

  if (field.type === 'enum') {
    return (
      <label className="flex min-h-[104px] flex-col gap-3 rounded-2xl border border-slate-100 bg-slate-50/70 p-4">
        {commonLabel}
        <select
          value={String(value ?? '')}
          onChange={(event) => onChange(event.target.value)}
          className="rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
        >
          {(field.enum || []).map((item) => (
            <option key={item} value={item}>
              {field.enum_labels?.[item] || item}
            </option>
          ))}
        </select>
      </label>
    );
  }

  const isSecret = field.key === 'AdminPassword' || field.key === 'ServerPassword';
  const inputType = isSecret ? 'password' : field.type === 'int' || field.type === 'float' ? 'number' : 'text';
  const step = field.type === 'float' ? '0.1' : '1';
  const isLongText = field.key.includes('Description') || field.type === 'list';

  return (
    <label className={`flex flex-col gap-3 rounded-2xl border border-slate-100 bg-slate-50/70 p-4 ${isLongText ? 'md:col-span-2' : ''}`}>
      {commonLabel}
      {isLongText ? (
        <textarea
          aria-label={field.label || field.key}
          value={String(value ?? '')}
          onChange={(event) => onChange(event.target.value)}
          rows={3}
          className="resize-none rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
        />
      ) : (
        <input
          aria-label={field.label || field.key}
          type={inputType}
          step={step}
          min={field.min}
          max={field.max}
          value={String(value ?? '')}
		  disabled={Boolean(clearSecret)}
		  placeholder={isSecret && secretConfigured ? '留空以保留现有密码' : undefined}
          onChange={(event) => {
            if (field.type === 'int' || field.type === 'float') {
              onChange(Number(event.target.value));
            } else {
              onChange(event.target.value);
            }
          }}
          className="rounded-xl border border-slate-200 bg-white p-3 text-xs font-semibold text-slate-700 focus:border-sky-500 focus:outline-none"
        />
      )}
	  {isSecret && secretConfigured && (
		<span className="inline-flex items-center gap-2 text-[11px] font-semibold text-rose-600">
		  <input
			type="checkbox"
			aria-label={`清除${field.label || field.key}`}
			checked={Boolean(clearSecret)}
			onChange={(event) => onClearSecret?.(event.target.checked)}
			className="h-4 w-4 rounded border-slate-300 text-rose-500 focus:ring-rose-500"
		  />
		  清除已保存的密码
		</span>
	  )}
    </label>
  );
};
