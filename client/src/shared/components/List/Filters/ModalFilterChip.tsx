import { DismissTiny } from "@/shared/assets/icons";

export type ModalFilterChipProps = {
  filterValue: string;
  /** Left-side pill — the filter category name (e.g. "Hashrate"). Matches FilterChip layout. */
  typeLabel: string;
  /** Middle text — describes the active condition (e.g. "≥ 50 TH/s AND ≤ 200 TH/s"). */
  condition: string;
  onEdit: () => void;
  onClear: () => void;
};

/**
 * Pill-style chip for filters whose editing surface is a modal (numeric range,
 * subnet textarea) rather than an inline popover. Layout mirrors `FilterChip`:
 * orange type pill on the left, condition + clear on the right side.
 */
const ModalFilterChip = ({ filterValue, typeLabel, condition, onEdit, onClear }: ModalFilterChipProps) => {
  return (
    <div
      className="relative inline-flex max-w-[min(18rem,calc(100vw-3rem))] shrink-0"
      data-testid={`active-filter-${filterValue}`}
    >
      <div className="inline-flex min-w-0 items-stretch overflow-hidden rounded-3xl text-emphasis-300">
        <button
          type="button"
          onClick={onEdit}
          className="flex shrink-0 cursor-pointer items-center bg-intent-warning-fill px-3 py-1 whitespace-nowrap text-text-base-contrast-static hover:opacity-90"
          data-testid={`active-filter-${filterValue}-type`}
        >
          {typeLabel}
        </button>
        <button
          type="button"
          onClick={onEdit}
          className="flex min-w-0 cursor-pointer items-center bg-core-primary-5 px-3 py-1 text-text-primary hover:opacity-80"
          data-testid={`active-filter-${filterValue}-edit`}
          aria-haspopup="dialog"
          title={condition}
        >
          <span className="min-w-0 truncate whitespace-nowrap">{condition}</span>
        </button>
        <button
          type="button"
          onClick={onClear}
          className="flex shrink-0 cursor-pointer items-center bg-core-primary-5 py-1 pr-3 pl-1 text-text-primary hover:opacity-80"
          data-testid={`active-filter-${filterValue}-clear`}
          aria-label={`Clear ${typeLabel} filter`}
        >
          <DismissTiny />
        </button>
      </div>
    </div>
  );
};

export default ModalFilterChip;
