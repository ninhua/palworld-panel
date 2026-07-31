import React from 'react';
import { useLocation } from 'react-router-dom';
import { Menu, Megaphone, RefreshCw, Save, ServerCrash } from 'lucide-react';
import { useServerStore } from '../../store/useServerStore';
import { getRouteMetaByPathname } from '../../routes';
import { appConfig } from '../../config/defaults';
import { useI18n, type TranslationKey } from '../../i18n';
import { LanguageSwitcher } from '../ui/LanguageSwitcher';

interface HeaderProps {
  onMenuClick: () => void;
  onAnnounceClick: () => void;
  onSaveClick: () => void;
  onRestartClick: () => void;
}

const groupLabels: Record<string, TranslationKey> = { setup: 'header.setup', workspace: 'header.workspace', world: 'header.world', system: 'header.system' };
const desktopShellMediaQuery = '(min-width: 900px)';

export const Header: React.FC<HeaderProps> = ({ onMenuClick, onAnnounceClick, onSaveClick, onRestartClick }) => {
  const { t } = useI18n();
  const { autoRefresh, setAutoRefresh, triggerRefresh, isSidebarCollapsed, setIsSidebarCollapsed } = useServerStore();
  const location = useLocation();
  const routeMeta = getRouteMetaByPathname(location.pathname);
  const title = routeMeta ? (routeMeta.titleKey ? t(routeMeta.titleKey) : routeMeta.title) : t('route.panel');
  const groupLabel = routeMeta ? t(groupLabels[routeMeta.navGroup]) : appConfig.brand.toUpperCase();

  const handleNavigationClick = () => {
    if (window.matchMedia(desktopShellMediaQuery).matches) {
      setIsSidebarCollapsed(!isSidebarCollapsed);
      return;
    }
    onMenuClick();
  };

  return (
    <header className="pp-topbar">
      <div className="pp-topbar__inner">
        <div className="pp-row pp-grow">
          <button type="button" onClick={handleNavigationClick} className="pp-button" aria-label={t('header.toggleNavigation')}><Menu size={17} /></button>
          <div className="pp-grow">
            <div className="pp-topbar__eyebrow">{groupLabel}</div>
            <h2 className="pp-topbar__title pp-truncate">{title}</h2>
          </div>
        </div>
        <div className="pp-topbar__actions">
          <LanguageSwitcher compact className="!hidden xl:!inline-flex" />
          <button type="button" onClick={() => setAutoRefresh(!autoRefresh)} className="pp-button !hidden sm:!inline-flex">
            <span className={`h-2 w-2 rounded-full ${autoRefresh ? 'bg-blue-500' : 'bg-slate-300'}`} />
            <span>{autoRefresh ? t('header.autoRefresh') : t('header.paused')}</span>
          </button>
          <button type="button" onClick={triggerRefresh} title={t('header.syncTitle')} className="pp-button"><RefreshCw size={14} /><span>{t('header.sync')}</span></button>
          <button type="button" onClick={onSaveClick} title={t('header.saveWorld')} className="pp-button !hidden md:!inline-flex"><Save size={14} /><span>{t('common.save')}</span></button>
          <button type="button" onClick={onRestartClick} title={t('header.restartServer')} className="pp-button !hidden text-rose-600 md:!inline-flex"><ServerCrash size={14} /><span>{t('header.restart')}</span></button>
          <button type="button" onClick={onAnnounceClick} title={t('header.announcement')} className="pp-button pp-btn--primary !hidden sm:!inline-flex"><Megaphone size={14} /><span>{t('header.broadcast')}</span></button>
        </div>
      </div>
    </header>
  );
};
