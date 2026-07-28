import type { PalTemplateInfo } from '../../api/starterGift';

export interface PalTemplateFilterState {
  index: string;
  category: string;
  overallGrade: string;
  usageCategory: string;
  classificationTag: string;
  graduationUse: string;
  status: string;
}

export const createEmptyPalTemplateFilters = (): PalTemplateFilterState => ({
  index: 'all',
  category: 'all',
  overallGrade: 'all',
  usageCategory: 'all',
  classificationTag: 'all',
  graduationUse: 'all',
  status: 'all',
});

export const palTemplateMatchesFilters = (template: PalTemplateInfo | undefined, filters: PalTemplateFilterState) => {
  const category = template?.category || '未分类';
  if (filters.index !== 'all' && !template?.index_names.includes(filters.index)) return false;
  if (filters.category !== 'all' && category !== filters.category) return false;
  if (filters.overallGrade !== 'all' && template?.overall_grade !== filters.overallGrade) return false;
  if (filters.usageCategory !== 'all' && template?.usage_category !== filters.usageCategory) return false;
  if (filters.classificationTag !== 'all' && !template?.classification_tags.includes(filters.classificationTag)) return false;
  if (filters.graduationUse !== 'all' && !template?.graduation_uses.includes(filters.graduationUse)) return false;
  switch (filters.status) {
    case 'graduation': return Boolean(template?.graduation_pal);
    case 'current-graduation': return Boolean(template?.current_graduation_use);
    case 'transitional': return Boolean(template?.transitional);
    case 'ordinary': return Boolean(template && !template.graduation_pal && !template.current_graduation_use && !template.transitional);
    case 'unindexed': return Boolean(template && template.index_names.length === 0);
    default: return true;
  }
};
