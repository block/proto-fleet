import clsx from "clsx";

import Button, { sizes, variants } from "@/shared/components/Button";

interface NoFilterResultsEmptyStateProps {
  hasActiveFilters?: boolean;
  onClearFilters?: () => void;
  className?: string;
}

const NoFilterResultsEmptyState = ({
  hasActiveFilters = false,
  onClearFilters,
  className,
}: NoFilterResultsEmptyStateProps) => (
  <div className={clsx("flex min-h-[220px] w-full flex-col items-center justify-center py-14 text-center", className)}>
    <div className="text-heading-200 text-text-primary">No results</div>
    {hasActiveFilters ? (
      <>
        <p className="mt-1 text-400 text-text-primary-70">Try adjusting or clearing your filters.</p>
        {onClearFilters ? (
          <Button
            className="mt-6"
            variant={variants.secondary}
            size={sizes.base}
            testId="clear-all-filters-button"
            onClick={onClearFilters}
          >
            Clear all filters
          </Button>
        ) : null}
      </>
    ) : null}
  </div>
);

export default NoFilterResultsEmptyState;
