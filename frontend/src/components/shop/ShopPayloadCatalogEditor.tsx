import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Minus, Plus, Search, Trash2 } from 'lucide-react';
import { starterGiftApi, type PalTemplateInfo, type StarterGiftCatalogItem } from '../../api/starterGift';
import type { ShopDeliveryMode } from '../../api/shop';
import { PalTemplateFilters } from '../gm/PalTemplateFilters';
import { createEmptyPalTemplateFilters, palTemplateMatchesFilters } from '../gm/palTemplateFilterModel';

interface SelectedItem { item_id: string; count: number }

const payloadItems = (payload: Record<string, unknown>): SelectedItem[] => {
  const raw = payload.items;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((entry) => {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return [];
    const item = entry as Record<string, unknown>;
    const id = String(item.item_id || '').trim();
    const count = Number(item.count || 0);
    return id && Number.isSafeInteger(count) && count > 0 ? [{ item_id: id, count }] : [];
  });
};

const payloadTemplates = (payload: Record<string, unknown>) => {
  const raw = payload.pal_templates;
  return Array.isArray(raw) ? Array.from(new Set(raw.map((item) => String(item || '').trim()).filter(Boolean))) : [];
};

const templateLabel = (template: PalTemplateInfo) => template.pal_name?.trim() || template.nickname?.trim() || template.name;
const templateSearchText = (template: PalTemplateInfo) => [
  template.name, template.pal_id, template.pal_name, template.english_name, template.nickname,
  template.category, template.usage_category, template.overall_grade,
  ...template.classification_tags, ...template.passive_names, ...template.index_names,
].filter(Boolean).join(' ').toLowerCase();

export const ShopPayloadCatalogEditor: React.FC<{
  mode: ShopDeliveryMode;
  payload: Record<string, unknown>;
  onChange: (payload: Record<string, unknown>) => void;
}> = ({ mode, payload, onChange }) => {
  const catalogQuery = useQuery({
    queryKey: ['shop', 'product-editor-catalog'],
    queryFn: starterGiftApi.get,
    enabled: mode !== 'manual',
    staleTime: 5 * 60 * 1000,
  });
  const [items, setItems] = useState<SelectedItem[]>(() => payloadItems(payload));
  const [templates, setTemplates] = useState<string[]>(() => payloadTemplates(payload));
  const [itemSearch, setItemSearch] = useState('');
  const [templateSearch, setTemplateSearch] = useState('');
  const [templateFilters, setTemplateFilters] = useState(createEmptyPalTemplateFilters);

  useEffect(() => {
    setItems(payloadItems(payload));
    setTemplates(payloadTemplates(payload));
  }, [payload]);

  const emitItems = (next: SelectedItem[]) => {
    setItems(next);
    onChange({ items: next.map((item) => ({ item_id: item.item_id, count: item.count })) });
  };
  const emitTemplates = (next: string[]) => {
    setTemplates(next);
    onChange({ pal_templates: next });
  };

  const itemCatalog = catalogQuery.data?.item_catalog || [];
  const palCatalog = catalogQuery.data?.templates || [];
  const indexes = catalogQuery.data?.template_indexes || [];
  const itemByID = useMemo(() => new Map(itemCatalog.map((item) => [item.id, item])), [itemCatalog]);
  const visibleItems = useMemo(() => {
    const needle = itemSearch.trim().toLowerCase();
    return itemCatalog.filter((item) => !needle || `${item.id} ${item.name}`.toLowerCase().includes(needle)).slice(0, 300);
  }, [itemCatalog, itemSearch]);
  const visibleTemplates = useMemo(() => {
    const needle = templateSearch.trim().toLowerCase();
    return palCatalog
      .filter((template) => !template.parse_error)
      .filter((template) => palTemplateMatchesFilters(template, templateFilters))
      .filter((template) => !needle || templateSearchText(template).includes(needle))
      .sort((left, right) => templateLabel(left).localeCompare(templateLabel(right), 'zh-CN'))
      .slice(0, 300);
  }, [palCatalog, templateFilters, templateSearch]);

  if (mode === 'manual') return null;
  if (catalogQuery.isLoading) return <div className="rounded-2xl border border-dashed border-slate-200 p-6 text-center text-xs font-semibold text-slate-400">正在加载服务器目录…</div>;
  if (catalogQuery.isError) return <div className="rounded-2xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700">目录读取失败，请检查 PalDefender 与模板目录。</div>;

  if (mode === 'paldefender_items') {
    return (
      <div className="grid gap-4 lg:grid-cols-2">
        <section className="rounded-2xl border border-slate-200 p-3">
          <div className="relative"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={itemSearch} onChange={(event) => setItemSearch(event.target.value)} placeholder="搜索物品中文名或内部ID" className="pp-input w-full pl-9" /></div>
          <div className="mt-3 max-h-72 space-y-1 overflow-y-auto">
            {visibleItems.map((item: StarterGiftCatalogItem) => (
              <button key={item.id} type="button" onClick={() => {
                if (items.some((selected) => selected.item_id === item.id)) return;
                emitItems([...items, { item_id: item.id, count: 1 }]);
              }} className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left hover:bg-slate-50">
                <span><strong className="block text-xs text-slate-700">{item.name || item.id}</strong><span className="font-mono text-[10px] text-slate-400">{item.id}</span></span><Plus size={14} className="text-sky-600" />
              </button>
            ))}
          </div>
        </section>
        <section className="rounded-2xl border border-sky-100 bg-sky-50/40 p-3">
          <h3 className="text-xs font-bold text-slate-700">商品发放内容（{items.length} 种）</h3>
          <div className="mt-3 space-y-2">
            {items.length === 0 ? <p className="py-8 text-center text-xs text-slate-400">从左侧物品列表添加。</p> : items.map((item) => (
              <div key={item.item_id} className="flex items-center gap-2 rounded-xl bg-white p-2 shadow-sm">
                <div className="min-w-0 flex-1"><strong className="block truncate text-xs text-slate-700">{itemByID.get(item.item_id)?.name || item.item_id}</strong><span className="font-mono text-[10px] text-slate-400">{item.item_id}</span></div>
                <button type="button" onClick={() => emitItems(items.map((current) => current.item_id === item.item_id ? { ...current, count: Math.max(1, current.count - 1) } : current))} className="rounded-lg p-1.5 hover:bg-slate-100"><Minus size={13} /></button>
                <input type="number" min={1} max={2147483647} value={item.count} onChange={(event) => emitItems(items.map((current) => current.item_id === item.item_id ? { ...current, count: Math.max(1, Number(event.target.value) || 1) } : current))} className="pp-input w-20 text-center" />
                <button type="button" onClick={() => emitItems(items.filter((current) => current.item_id !== item.item_id))} className="rounded-lg p-1.5 text-rose-600 hover:bg-rose-50"><Trash2 size={13} /></button>
              </div>
            ))}
          </div>
        </section>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <PalTemplateFilters templates={palCatalog} indexes={indexes} value={templateFilters} onChange={setTemplateFilters} />
      <div className="relative"><Search size={14} className="absolute left-3 top-3 text-slate-400" /><input value={templateSearch} onChange={(event) => setTemplateSearch(event.target.value)} placeholder="搜索帕鲁名、PalID、模板名、用途或标签" className="pp-input w-full pl-9" /></div>
      <div className="grid gap-4 lg:grid-cols-2">
        <section className="max-h-80 overflow-y-auto rounded-2xl border border-slate-200 p-2">
          {visibleTemplates.map((template) => (
            <button key={template.name} type="button" onClick={() => !templates.includes(template.name) && emitTemplates([...templates, template.name])} className="flex w-full items-center justify-between rounded-xl px-3 py-2 text-left hover:bg-slate-50">
              <span className="min-w-0"><strong className="block truncate text-xs text-slate-700">{templateLabel(template)}</strong><span className="block truncate font-mono text-[10px] text-slate-400">{template.pal_id || '未知PalID'} · {template.name}</span><span className="mt-1 block truncate text-[10px] text-slate-400">{[template.category, template.overall_grade, template.usage_category].filter(Boolean).join(' · ') || '未分类'}</span></span><Plus size={14} className="shrink-0 text-violet-600" />
            </button>
          ))}
        </section>
        <section className="rounded-2xl border border-violet-100 bg-violet-50/40 p-3">
          <h3 className="text-xs font-bold text-slate-700">商品发放模板（{templates.length} 个）</h3>
          <div className="mt-3 space-y-2">{templates.length === 0 ? <p className="py-8 text-center text-xs text-slate-400">从左侧筛选结果添加模板。</p> : templates.map((name) => {
            const template = palCatalog.find((item) => item.name === name);
            return <div key={name} className="flex items-center gap-2 rounded-xl bg-white p-2 shadow-sm"><div className="min-w-0 flex-1"><strong className="block truncate text-xs text-slate-700">{template ? templateLabel(template) : name}</strong><span className="block truncate font-mono text-[10px] text-slate-400">{name}</span></div><button type="button" onClick={() => emitTemplates(templates.filter((item) => item !== name))} className="rounded-lg p-1.5 text-rose-600 hover:bg-rose-50"><Trash2 size={13} /></button></div>;
          })}</div>
        </section>
      </div>
    </div>
  );
};
