import type { PalTemplateInfo } from '../../api/starterGift';

export interface PalTemplateFilterState {
  index: string[];
  category: string[];
  overallGrade: string[];
  usageCategory: string[];
  classificationTag: string[];
  graduationUse: string[];
  status: string[];
}

export const createEmptyPalTemplateFilters = (): PalTemplateFilterState => ({
  index: [],
  category: [],
  overallGrade: [],
  usageCategory: [],
  classificationTag: [],
  graduationUse: [],
  status: [],
});

const matchesAny = (selected: string[], values: Array<string | undefined>) =>
  selected.length === 0 || values.some((item) => item !== undefined && selected.includes(item));

export const palTemplateUsageTerms = (value?: string) =>
  (value ?? '').split(/[+＋]/).map((item) => item.trim()).filter(Boolean);

export const palTemplateMatchesFilters = (template: PalTemplateInfo | undefined, filters: PalTemplateFilterState) => {
  const category = template?.category || '未分类';
  if (!matchesAny(filters.index, template?.index_names ?? [])) return false;
  if (!matchesAny(filters.category, [category])) return false;
  if (!matchesAny(filters.overallGrade, [template?.overall_grade])) return false;
  if (!matchesAny(filters.usageCategory, palTemplateUsageTerms(template?.usage_category))) return false;
  if (!matchesAny(filters.classificationTag, template?.classification_tags ?? [])) return false;
  if (!matchesAny(filters.graduationUse, template?.graduation_uses ?? [])) return false;
  if (filters.status.length === 0) return true;
  return filters.status.some((status) => {
    switch (status) {
      case 'graduation': return Boolean(template?.graduation_pal);
      case 'current-graduation': return Boolean(template?.current_graduation_use);
      case 'transitional': return Boolean(template?.transitional);
      case 'ordinary': return Boolean(template && !template.graduation_pal && !template.current_graduation_use && !template.transitional);
      case 'unindexed': return Boolean(template && template.index_names.length === 0);
      default: return false;
    }
  });
};
