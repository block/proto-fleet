import { ReactNode, useEffect, useState } from "react";
import clsx from "clsx";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { sizes } from "@/shared/components/ButtonGroup";

interface ActionBarProps {
  className?: string;
  selectedItems: string[];
  renderActions: (setHidden: (hidden: boolean) => void) => ReactNode;
  onClose?: () => void;
}

const ActionBar = ({
  className,
  selectedItems,
  renderActions,
  onClose,
}: ActionBarProps) => {
  const [show, setShow] = useState(false);
  const [hidden, setHidden] = useState(false);

  useEffect(() => {
    setShow(selectedItems.length > 0);
  }, [selectedItems]);

  const handleClose = () => {
    setShow(false);
    onClose?.();
  };

  if (!show) {
    return null;
  }

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
      <div className="flex w-[calc(100vw-theme(spacing.24))] max-w-[640px] items-center justify-between gap-4 rounded-2xl bg-black p-3 shadow-300 dark:bg-surface-elevated-base phone:w-[calc(100vw-theme(spacing.4))]">
        <div className="flex items-center space-x-2">
          <Button
            className="bg-grayscale-white-10! text-grayscale-white-90!"
            prefixIcon={<DismissTiny />}
            variant={variants.secondary}
            size={sizes.compact}
            testId="close-button"
            onClick={handleClose}
          />
          <div className="w-full text-emphasis-300 text-grayscale-white-90">
            {selectedItems.length} miners selected
          </div>
        </div>
        <div className="flex flex-wrap justify-start gap-3">
          {renderActions(setHidden)}
        </div>
      </div>
    </div>
  );
};

export default ActionBar;
