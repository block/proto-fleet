// Shared formatter for the "X nouns" / "X of Y nouns" count line that sits
// beneath a list's filter row. When filters are active and the filtered count
// differs from the unfiltered total, it reads "X of Y nouns"; otherwise just
// "X nouns". Used by the List component's built-in total and by pages that
// render their own count line (e.g. the Racks grid, which isn't a List).
export interface ListCountLabelOptions {
  // Unfiltered total for the current scope. Omit when unknown — the label then
  // always renders the plain "X nouns" form.
  unfilteredTotal?: number;
  hasActiveFilters?: boolean;
  singular: string;
  plural: string;
}

export const formatListCountLabel = (total: number, options: ListCountLabelOptions): string => {
  const { unfilteredTotal, hasActiveFilters, singular, plural } = options;
  if (hasActiveFilters && unfilteredTotal !== undefined && total !== unfilteredTotal) {
    return `${total} of ${unfilteredTotal} ${unfilteredTotal === 1 ? singular : plural}`;
  }
  return `${total} ${total === 1 ? singular : plural}`;
};
