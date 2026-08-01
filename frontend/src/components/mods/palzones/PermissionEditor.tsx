import React, { useState } from 'react';
import {
  DAMAGE_TARGETS,
  PERMISSION_DEFINITIONS,
  PLAYER_WORLD_ACTIONS,
  type PalZonesDamageTarget,
  type PalZonesPermissions,
  type PalZonesRoleID,
  type PalZonesWorldAction,
} from './model';

interface Props {
  permissions: PalZonesPermissions;
  disabled: boolean;
  onChange: (permissions: PalZonesPermissions) => void;
}

const clone = (value: PalZonesPermissions): PalZonesPermissions => JSON.parse(JSON.stringify(value)) as PalZonesPermissions;

export const PermissionEditor: React.FC<Props> = ({ permissions, disabled, onChange }) => {
  const [roleID, setRoleID] = useState<PalZonesRoleID>('Player');
  const role = PERMISSION_DEFINITIONS.find((item) => item.id === roleID) ?? PERMISSION_DEFINITIONS[0];
  const entry = permissions[roleID] ?? {};
  const world = new Set(entry.world ?? []);
  const damage = new Set((entry.damage ?? []).filter((item): item is PalZonesDamageTarget => typeof item === 'string'));
  const multiplier = (entry.damage ?? []).find((item): item is { DamageMultiplier: number } => typeof item === 'object')?.DamageMultiplier ?? 1;

  const update = (nextWorld: PalZonesWorldAction[], nextDamage: Set<PalZonesDamageTarget>, nextMultiplier: number) => {
    const next = clone(permissions);
    next[roleID] = {
      ...(nextWorld.length ? { world: nextWorld } : {}),
      damage: [...DAMAGE_TARGETS.filter((target) => nextDamage.has(target.id)).map((target) => target.id), { DamageMultiplier: Math.max(0, nextMultiplier) }],
    };
    onChange(next);
  };

  return (
    <div className={'space-y-3 border-t border-slate-200 pt-3'}>
      <div className={'flex max-w-full gap-1 overflow-x-auto'}>{PERMISSION_DEFINITIONS.map((item) => <button key={item.id} type={'button'} onClick={() => setRoleID(item.id)} className={'shrink-0 px-2 py-1.5 text-[11px] font-bold ' + (roleID === item.id ? 'bg-sky-100 text-sky-800' : 'text-slate-500')}>{item.label}</button>)}</div>
      {roleID === 'Player' && <div className={'space-y-1.5'}>
        <p className={'text-[10px] font-black text-slate-500'}>非管理员可以</p>
        {PLAYER_WORLD_ACTIONS.map((action) => <button key={action.id} type={'button'} role={'switch'} aria-checked={world.has(action.id)} aria-label={'允许玩家' + action.label} disabled={disabled} onClick={() => {
          const next = new Set(world);
          if (next.has(action.id)) next.delete(action.id); else next.add(action.id);
          update(PLAYER_WORLD_ACTIONS.filter((item) => next.has(item.id)).map((item) => item.id), damage, multiplier);
        }} className={'flex w-full items-center justify-between px-2 py-1.5 text-left text-[11px] font-bold disabled:opacity-40'}><span>{action.label}</span><span className={'h-4 w-7 p-0.5 ' + (world.has(action.id) ? 'bg-emerald-500' : 'bg-slate-300')}><span className={'block h-3 w-3 bg-white transition ' + (world.has(action.id) ? 'translate-x-3' : '')} /></span></button>)}
      </div>}
      <div className={'space-y-1.5'}>
        <p className={'text-[10px] font-black text-slate-500'}>可以伤害</p>
        {DAMAGE_TARGETS.map((target) => <button key={target.id} type={'button'} role={'switch'} aria-checked={damage.has(target.id)} aria-label={'允许' + role.label + '伤害' + target.label} disabled={disabled} onClick={() => {
          const next = new Set(damage);
          if (next.has(target.id)) next.delete(target.id); else next.add(target.id);
          update([...world], next, multiplier);
        }} className={'flex w-full items-center justify-between px-2 py-1.5 text-left text-[11px] font-bold disabled:opacity-40'}><span>{target.label}</span><span className={'h-4 w-7 p-0.5 ' + (damage.has(target.id) ? 'bg-emerald-500' : 'bg-slate-300')}><span className={'block h-3 w-3 bg-white transition ' + (damage.has(target.id) ? 'translate-x-3' : '')} /></span></button>)}
      </div>
      <label className={'block text-[11px] font-bold text-slate-600'}>{role.label}伤害倍率<input aria-label={role.label + '伤害倍率'} type={'number'} min={0} step={0.1} value={multiplier} disabled={disabled} onChange={(event) => update([...world], damage, Number(event.target.value))} className={'mt-1 w-full border border-slate-200 bg-white px-3 py-2 text-xs'} /></label>
    </div>
  );
};
