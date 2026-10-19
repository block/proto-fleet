import clsx from "clsx";

import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ListPaginationProps {
  firstItemIndex: number;
  lastItemIndex: number;
  total?: number;
  hasPreviousPage: boolean;
  itemName: string;
  hasNextPage: boolean;
  loading?: boolean;
  className?: string;
  testId?: string;
  onPrevious: () => void;
  onNext: () => void;
}

const ListPagination = ({
  firstItemIndex,
  lastItemIndex,
  hasPreviousPage,
  total,
  itemName,
  hasNextPage,
  loading = false,
  className = "py-6",
  testId,
  onPrevious,
  onNext,
}: ListPaginationProps) => {
  if (total === 0 && !hasPreviousPage) return null;

  return (
    <div className={clsx("flex flex-col items-center gap-4", className)} data-testid={testId}>
      <span className="text-300 text-text-primary">
        {lastItemIndex < firstItemIndex ? (
          <>No {itemName} on this page</>
        ) : (
          <>
            Showing {firstItemIndex}–{lastItemIndex}
            {total === undefined ? "" : ` of ${total}`} {itemName}
          </>
        )}
      </span>
      <div className="flex gap-3">
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Previous page"
          prefixIcon={<ChevronDown className="rotate-90" />}
          onClick={onPrevious}
          disabled={loading || !hasPreviousPage}
        />
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Next page"
          prefixIcon={<ChevronDown className="rotate-270" />}
          onClick={onNext}
          disabled={loading || !hasNextPage}
        />
      </div>
    </div>
  );
};

export default ListPagination;
