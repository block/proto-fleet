import Button, { sizes, variants } from "@/shared/components/Button";

interface NoFilterResultsEmptyStateProps {
  hasActiveFilters?: boolean;
  onClearFilters?: () => void;
}

const NoFilterResultsEmptyState = ({ hasActiveFilters = false, onClearFilters }: NoFilterResultsEmptyStateProps) => (
  <div className="flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center">
    <div className="text-heading-200 text-text-primary">No results</div>
    {hasActiveFilters && (
      <>
        <p className="mt-1 text-400 text-text-primary-70">Try adjusting or clearing your filters.</p>
        {onClearFilters && (
          <Button
            className="mt-6"
            variant={variants.secondary}
            size={sizes.base}
            testId="clear-all-filters-button"
            onClick={onClearFilters}
          >
            Clear all filters
          </Button>
        )}
      </>
    )}
  </div>
);

export default NoFilterResultsEmptyState;
