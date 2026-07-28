import { describe, expect, it } from 'vitest';
import type { PalTemplateInfo } from '../../api/starterGift';
import { createEmptyPalTemplateFilters, palTemplateMatchesFilters } from './palTemplateFilterModel';

const template: PalTemplateInfo = {
  name: 'WORK__Anubis.json',
  pal_id: 'Anubis',
  pal_name: '阿努比斯',
  english_name: 'Anubis',
  category: '工作',
  usage_category: '手工作业＋采矿＋搬运',
  overall_grade: '常用毕业',
  index_names: ['pal-template-index.json'],
  classification_tags: ['工作:手工作业', '毕业:工作'],
  graduation_pal: true,
  graduation_uses: ['工作'],
  current_graduation_use: true,
  transitional: false,
  passive_names: ['工匠精神'],
};

describe('palTemplateMatchesFilters', () => {
  it('supports every rich index dimension together', () => {
    expect(palTemplateMatchesFilters(template, {
      index: ['pal-template-index.json'],
      category: ['工作'],
      overallGrade: ['常用毕业'],
      usageCategory: ['手工作业'],
      classificationTag: ['毕业:工作'],
      graduationUse: ['工作'],
      status: ['current-graduation'],
    })).toBe(true);
  });

  it('rejects a mismatched dimension and supports unindexed status', () => {
    expect(palTemplateMatchesFilters(template, { ...createEmptyPalTemplateFilters(), overallGrade: ['普通'] })).toBe(false);
    expect(palTemplateMatchesFilters({ ...template, index_names: [] }, { ...createEmptyPalTemplateFilters(), status: ['unindexed'] })).toBe(true);
  });

  it('uses OR within one dimension and AND across dimensions', () => {
    expect(palTemplateMatchesFilters(template, {
      ...createEmptyPalTemplateFilters(),
      usageCategory: ['播种', '手工作业'],
      overallGrade: ['常用毕业'],
    })).toBe(true);
    expect(palTemplateMatchesFilters(template, {
      ...createEmptyPalTemplateFilters(),
      usageCategory: ['播种', '手工作业'],
      overallGrade: ['普通'],
    })).toBe(false);
  });
});
