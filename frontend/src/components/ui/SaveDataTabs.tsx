import React from 'react';
import { Home, Map as MapIcon, Network, Sword, UserCog, Users } from 'lucide-react';
import { NavLink } from 'react-router-dom';

const tabs = [
  { path: '/player-center', label: '玩家中心', icon: <UserCog size={15} /> },
  { path: '/world', label: '玩家档案', icon: <Users size={15} /> },
  { path: '/player-summary', label: '玩家概览', icon: <Users size={15} /> },
  { path: '/guilds', label: '公会', icon: <Network size={15} /> },
  { path: '/bases', label: '基地', icon: <Home size={15} /> },
  { path: '/pal-inventory', label: '帕鲁仓库', icon: <Sword size={15} /> },
  { path: '/breeding', label: '配种实验室', icon: <Sword size={15} /> },
  { path: '/map', label: '世界地图', icon: <MapIcon size={15} /> },
];

export const SaveDataTabs: React.FC = () => (
  <nav className="flex max-w-full gap-1.5 overflow-x-auto rounded-xl border border-slate-200/80 bg-white p-1.5 shadow-sm" aria-label="存档数据分类">
    {tabs.map((tab) => (
      <NavLink
        key={tab.path}
        to={tab.path}
        className={({ isActive }) =>
          `inline-flex min-h-9 shrink-0 items-center gap-2 rounded-lg px-3.5 py-2 text-xs font-bold transition-colors ${
            isActive
              ? 'bg-sky-100 text-sky-800 shadow-sm ring-1 ring-sky-200'
              : 'text-slate-500 hover:bg-slate-50 hover:text-slate-800'
          }`
        }
      >
        {tab.icon}
        {tab.label}
      </NavLink>
    ))}
  </nav>
);
