import React, { useMemo } from 'react';
import type { PalTemplateIndexInfo, PalTemplateInfo } from '../../api/starterGift';
import type { PalTemplateFilterState } from './palTemplateFilterModel';

const uniqueSorted = (values: Array<string | undefined>) =>
  Array.from(new Set(values.filter((value): value is string => Boolean(value?.trim()))))
    .sort((left, right) => left.localeCompare(right, 'zh-CN'));

const SelectField: React.FC<{
  label: string;
  value: string;
  allLabel: string;
  options: string[];
  onChange: (value: string) => void;
}> = ({ label, value, allLabel, options, onChange }) => (
  <label className="min-w-0 text-[10px] font-black text-slate-500">
    <span className="mb-1 block truncate">{label}</span>
    <select aria-label={label} value={value} onChange={(event) => onChange(event.target.value)} className="w-full rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-[11px] font-semibold text-slate-700 focus:border-violet-400 focus:outline-none">
      <option value="all">{allLabel}</option>
      {options.map((option) => <option key={option} value={option}>{option}</option>)}
    </select>
  </label>
);

export const PalTemplateFilters: React.FC<{
  templates: PalTemplateInfo[];
  indexes: PalTemplateIndexInfo[];
  value: PalTemplateFilterState;
  onChange: (value: PalTemplateFilterState) => void;
}> = ({ templates, indexes, value, onChange }) => {
  const options = useMemo(() => ({
    categories: uniqueSorted(templates.map((template) => template.category || '未分类')),
    grades: uniqueSorted(templates.map((template) => template.overall_grade)),
    usages: uniqueSorted(templates.map((template) => template.usage_category)),
    tags: uniqueSorted(templates.flatMap((template) => template.classification_tags)),
    graduationUses: uniqueSorted(templates.flatMap((template) => template.graduation_uses)),
  }), [templates]);
  const update = (key: keyof PalTemplateFilterState, next: string) => onChange({ ...value, [key]: next });

  return (
    <section aria-label="帕鲁模板组合筛选" className="rounded-2xl border border-slate-200 bg-slate-50/70 p-3">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-7">
        <label className="min-w-0 text-[10px] font-black text-slate-500">
          <span className="mb-1 block truncate">索引文件</span>
          <select aria-label="索引文件" value={value.index} onChange={(event) => update('index', event.target.value)} className="w-full rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-[11px] font-semibold text-slate-700 focus:border-violet-400 focus:outline-none">
            <option value="all">全部索引</option>
            {indexes.map((index) => <option key={index.name} value={index.name}>{index.label || index.name}（{index.count}）</option>)}
          </select>
        </label>
        <SelectField label="模板分类" value={value.category} onChange={(next) => update('category', next)} allLabel="全部分类" options={options.categories} />
        <SelectField label="综合分级" value={value.overallGrade} onChange={(next) => update('overallGrade', next)} allLabel="全部分级" options={options.grades} />
        <SelectField label="用途分类" value={value.usageCategory} onChange={(next) => update('usageCategory', next)} allLabel="全部用途" options={options.usages} />
        <SelectField label="分类标签" value={value.classificationTag} onChange={(next) => update('classificationTag', next)} allLabel="全部标签" options={options.tags} />
        <SelectField label="毕业用途" value={value.graduationUse} onChange={(next) => update('graduationUse', next)} allLabel="全部毕业用途" options={options.graduationUses} />
        <label className="min-w-0 text-[10px] font-black text-slate-500">
          <span className="mb-1 block truncate">模板状态</span>
          <select aria-label="模板状态" value={value.status} onChange={(event) => update('status', event.target.value)} className="w-full rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-[11px] font-semibold text-slate-700 focus:border-violet-400 focus:outline-none">
            <option value="all">全部状态</option>
            <option value="graduation">毕业帕鲁</option>
            <option value="current-graduation">当前模板为毕业用途</option>
            <option value="transitional">低阶与过渡</option>
            <option value="ordinary">普通模板</option>
            <option value="unindexed">未命中索引</option>
          </select>
        </label>
      </div>
    </section>
  );
};
