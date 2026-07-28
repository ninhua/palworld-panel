import React, { useMemo } from 'react';
import type { PalTemplateIndexInfo, PalTemplateInfo } from '../../api/starterGift';
import { createEmptyPalTemplateFilters, palTemplateUsageTerms, type PalTemplateFilterState } from './palTemplateFilterModel';

const uniqueSorted = (values: Array<string | undefined>) =>
  Array.from(new Set(values.filter((value): value is string => Boolean(value?.trim()))))
    .sort((left, right) => left.localeCompare(right, 'zh-CN'));

const MultiSelectField: React.FC<{
  label: string;
  values: string[];
  options: Array<{ value: string; label: string }>;
  onChange: (values: string[]) => void;
}> = ({ label, values, options, onChange }) => {
  const toggle = (option: string) => {
    onChange(values.includes(option) ? values.filter((value) => value !== option) : [...values, option]);
  };
  const summary = values.length === 0 ? `全部${label}` : values.length === 1
    ? options.find((option) => option.value === values[0])?.label ?? values[0]
    : `已选 ${values.length} 项`;

  return (
    <details className="group relative min-w-0 text-[10px] font-black text-slate-500">
      <summary className="list-none cursor-pointer">
        <span className="mb-1 block truncate">{label}</span>
        <span className="flex min-h-9 items-center justify-between rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-[11px] font-semibold text-slate-700 group-open:border-violet-400">
          <span className="truncate">{summary}</span>
          <span aria-hidden="true" className="ml-2 text-slate-400">⌄</span>
        </span>
      </summary>
      <div className="absolute z-20 mt-1 max-h-64 w-full min-w-48 overflow-y-auto rounded-xl border border-slate-200 bg-white p-1.5 shadow-lg">
        {options.map((option) => (
          <label key={option.value} className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-[11px] font-semibold text-slate-700 hover:bg-slate-50">
            <input
              type="checkbox"
              checked={values.includes(option.value)}
              onChange={() => toggle(option.value)}
              className="size-3.5 rounded border-slate-300 text-violet-600 focus:ring-violet-500"
            />
            <span className="truncate">{option.label}</span>
          </label>
        ))}
      </div>
    </details>
  );
};

export const PalTemplateFilters: React.FC<{
  templates: PalTemplateInfo[];
  indexes: PalTemplateIndexInfo[];
  value: PalTemplateFilterState;
  onChange: (value: PalTemplateFilterState) => void;
}> = ({ templates, indexes, value, onChange }) => {
  const options = useMemo(() => ({
    categories: uniqueSorted(templates.map((template) => template.category || '未分类')),
    grades: uniqueSorted(templates.map((template) => template.overall_grade)),
    usages: uniqueSorted(templates.flatMap((template) => palTemplateUsageTerms(template.usage_category))),
    tags: uniqueSorted(templates.flatMap((template) => template.classification_tags)),
    graduationUses: uniqueSorted(templates.flatMap((template) => template.graduation_uses)),
  }), [templates]);
  const update = (key: keyof PalTemplateFilterState, next: string[]) => onChange({ ...value, [key]: next });
  const hasFilters = Object.values(value).some((selected) => selected.length > 0);
  const toOptions = (items: string[]) => items.map((item) => ({ value: item, label: item }));

  return (
    <section aria-label="帕鲁模板组合筛选" className="rounded-2xl border border-slate-200 bg-slate-50/70 p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <p className="text-[10px] font-bold text-slate-500">同一栏可多选，不同栏组合筛选</p>
        <button
          type="button"
          disabled={!hasFilters}
          onClick={() => onChange(createEmptyPalTemplateFilters())}
          className="rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-[10px] font-black text-slate-600 disabled:cursor-not-allowed disabled:opacity-40"
        >
          一键清空筛选
        </button>
      </div>
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-7">
        <MultiSelectField
          label="索引文件"
          values={value.index}
          options={indexes.map((index) => ({ value: index.name, label: `${index.label || index.name}（${index.count}）` }))}
          onChange={(next) => update('index', next)}
        />
        <MultiSelectField label="模板分类" values={value.category} onChange={(next) => update('category', next)} options={toOptions(options.categories)} />
        <MultiSelectField label="综合分级" values={value.overallGrade} onChange={(next) => update('overallGrade', next)} options={toOptions(options.grades)} />
        <MultiSelectField label="用途分类" values={value.usageCategory} onChange={(next) => update('usageCategory', next)} options={toOptions(options.usages)} />
        <MultiSelectField label="分类标签" values={value.classificationTag} onChange={(next) => update('classificationTag', next)} options={toOptions(options.tags)} />
        <MultiSelectField label="毕业用途" values={value.graduationUse} onChange={(next) => update('graduationUse', next)} options={toOptions(options.graduationUses)} />
        <MultiSelectField
          label="模板状态"
          values={value.status}
          onChange={(next) => update('status', next)}
          options={[
            { value: 'graduation', label: '毕业帕鲁' },
            { value: 'current-graduation', label: '当前模板为毕业用途' },
            { value: 'transitional', label: '低阶与过渡' },
            { value: 'ordinary', label: '普通模板' },
            { value: 'unindexed', label: '未命中索引' },
          ]}
        />
      </div>
    </section>
  );
};
