import { describe, expect, it } from 'vitest';
import { RAIL } from './filterSections';

describe('the ai_archetype facet', () => {
  it('has no standalone rail entry — FilterModal folds it into the Role pane instead', () => {
    expect(RAIL.find((e) => e.key === 'ai_archetype')).toBeUndefined();
    expect(RAIL.find((e) => e.facetParam === 'ai_archetype')).toBeUndefined();
  });
});
