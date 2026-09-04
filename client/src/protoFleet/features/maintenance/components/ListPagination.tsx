import { ChevronDown } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ListPaginationProps {
  currentPage: number;
  pageSize: number;
  visibleCount: number;
  total: number;
  itemName: string;
  hasNextPage: boolean;
  loading?: boolean;
  onPrevious: () => void;
  onNext: () => void;
}

const ListPagination = ({
  currentPage,
  pageSize,
  visibleCount,
  total,
  itemName,
  hasNextPage,
  loading = false,
  onPrevious,
  onNext,
}: ListPaginationProps) => {
  if (total <= 0) return null;
  const firstItem = currentPage * pageSize + 1;
  const lastItem = currentPage * pageSize + visibleCount;

  return (
    <div className="flex flex-col items-center gap-4 py-6">
      <span className="text-300 text-text-primary">
        Showing {firstItem}–{lastItem} of {total} {itemName}
      </span>
      <div className="flex gap-3">
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          ariaLabel="Previous page"
          prefixIcon={<ChevronDown className="rotate-90" />}
          onClick={onPrevious}
          disabled={loading || currentPage === 0}
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
