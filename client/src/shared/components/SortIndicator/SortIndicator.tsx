import clsx from "clsx";

import { ArrowDown, ArrowUp } from "@/shared/assets/icons";
import { SORT_ASC, SORT_DESC, SortDirection } from "@/shared/components/List/types";

export interface SortIndicatorProps {
  /** Current sort direction. undefined means the column is not currently sorted. */
  direction?: SortDirection;
  /** Default sort direction for this column when first clicked. Used for hover preview. */
  defaultDirection?: SortDirection;
  /** Whether the parent element is being hovered. */
  isHovering?: boolean;
  /** Optional additional CSS classes. */
  className?: string;
}

/**
 * Displays a sort direction indicator with hover preview.
 * Always renders to reserve space and prevent layout shift.
 *
 * Behavior:
 * - Not sorted + not hovering: invisible placeholder
 * - Not sorted + hovering: grey arrow showing defaultDirection preview
 * - Sorted ASC + not hovering: primary up arrow
 * - Sorted ASC + hovering: grey down arrow (DESC preview)
 * - Sorted DESC + not hovering: primary down arrow
 * - Sorted DESC + hovering: grey up arrow (ASC preview)
 */
const SortIndicator = ({
  direction,
  defaultDirection = SORT_ASC,
  isHovering = false,
  className,
}: SortIndicatorProps) => {
  const isSorted = direction !== undefined;
  const isVisible = isHovering || isSorted;

  let ArrowIcon: typeof ArrowUp | typeof ArrowDown;
  let colorClass = "";

  if (isHovering) {
    // When hovering, show preview of what clicking will do
    const nextDirection = isSorted ? (direction === SORT_DESC ? SORT_ASC : SORT_DESC) : defaultDirection;
    ArrowIcon = nextDirection === SORT_ASC ? ArrowUp : ArrowDown;
    colorClass = "text-text-primary-50";
  } else if (isSorted) {
    ArrowIcon = direction === SORT_ASC ? ArrowUp : ArrowDown;
  } else {
    ArrowIcon = ArrowDown;
  }

  return (
    <div
      className={clsx("ml-1 inline-flex items-center", colorClass, !isVisible && "invisible", className)}
      aria-hidden="true"
    >
      <ArrowIcon />
    </div>
  );
};

export default SortIndicator;
