import { ReactNode, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { sizes } from "@/shared/components/ButtonGroup";
import { type SelectionMode } from "@/shared/components/List";

interface ActionBarProps {
  className?: string;
  /** IDs of currently selected items (used for count display in "subset" mode) */
  selectedItems: string[];
  /**
   * How items were selected:
   * - "all": user clicked "Select All" with no filters (targets entire fleet)
   * - "subset": user selected specific items or "Select All" with filters active
   * - "none": no selection (ActionBar will be hidden)
   * @default "subset"
   */
  selectionMode?: SelectionMode;
  /**
   * Total number of items in the fleet. Used to display accurate count when
   * selectionMode is "all", since selectedItems only contains visible page items.
   */
  totalCount?: number;
  selectionControls?: ReactNode;
  renderActions: (setHidden: (hidden: boolean) => void) => ReactNode;
  onClose?: () => void;
}

const ActionBar = ({
  className,
  selectedItems,
  selectionMode = "subset",
  totalCount,
  selectionControls,
  renderActions,
  onClose,
}: ActionBarProps) => {
  const [show, setShow] = useState(false);
  const [hidden, setHidden] = useState(false);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reveal action bar when selection grows; local `show` can also be cleared independently by the close button
    setShow(selectedItems.length > 0);
  }, [selectedItems]);

  const selectionText = useMemo(() => {
    const count = selectionMode === "all" ? (totalCount ?? selectedItems.length) : selectedItems.length;
    return `${count} miner${count === 1 ? "" : "s"} selected`;
  }, [selectionMode, selectedItems.length, totalCount]);

  const handleClose = () => {
    setShow(false);
    onClose?.();
  };

  if (!show) {
    return null;
  }

  const actionsClassName = clsx(
    "ml-auto flex items-center justify-end gap-3",
    "phone:col-start-2 phone:row-start-2 phone:ml-0 phone:justify-end",
    "tablet:col-start-2 tablet:row-start-2 tablet:ml-0 tablet:justify-end",
    selectionControls ? "" : "phone:col-span-2 tablet:col-span-2",
  );

  return (
    <div
      className={clsx(
        "flex justify-center",
        {
          invisible: hidden,
        },
        className,
      )}
      data-testid="action-bar"
    >
      <div className="flex w-[calc(100%-theme(spacing.24))] items-center gap-2 rounded-2xl bg-black px-3 py-3 shadow-300 dark:bg-surface-elevated-base phone:grid phone:w-[calc(100%-theme(spacing.4))] phone:grid-cols-[minmax(0,1fr),auto] phone:gap-x-3 phone:gap-y-1 phone:py-2 tablet:grid tablet:w-[calc(100%-theme(spacing.12))] tablet:grid-cols-[minmax(0,1fr),auto] tablet:gap-x-3 tablet:gap-y-1 tablet:py-2">
        <div className="flex min-w-0 items-center gap-2 phone:col-span-2 tablet:col-span-2">
          <Button
            className="bg-grayscale-white-10! text-grayscale-white-90!"
            prefixIcon={<DismissTiny />}
            variant={variants.secondary}
            size={sizes.compact}
            testId="close-button"
            onClick={handleClose}
          />
          <div className="text-emphasis-300 text-grayscale-white-90">{selectionText}</div>
        </div>
        {selectionControls ? (
          <div className="flex flex-wrap items-center gap-2 phone:row-start-2 phone:ml-10 tablet:row-start-2 tablet:ml-10">
            {selectionControls}
          </div>
        ) : null}
        <div className={actionsClassName}>{renderActions(setHidden)}</div>
      </div>
    </div>
  );
};

export default ActionBar;
