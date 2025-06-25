import { ReactNode, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { sizes } from "@/shared/components/ButtonGroup";

interface ActionBarProps {
  className?: string;
  selectedItems: string[];
  renderActions: (
    numberOfItems: number,
    setHidden: (hidden: boolean) => void,
  ) => ReactNode;
}

const ActionBar = ({
  className,
  selectedItems,
  renderActions,
}: ActionBarProps) => {
  const [show, setShow] = useState(false);

  useEffect(() => {
    setShow(selectedItems.length > 0);
  }, [selectedItems]);

  const [hidden, setHidden] = useState(false);

  const numberOfItems = useMemo(() => {
    return selectedItems.length;
  }, [selectedItems]);

  return (
    <>
      {show && (
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
          <div className="bg-sufrace-elevated-base/70 flex items-center justify-between rounded-2xl bg-grayscale-gray-87 p-3 shadow-300 phone:w-[calc(100vw-theme(spacing.4))]">
            <div className="flex items-center space-x-2">
              <Button
                className="bg-grayscale-white-10! text-grayscale-white-90!"
                prefixIcon={<DismissTiny />}
                variant={variants.secondary}
                size={sizes.compact}
                testId="close-button"
                onClick={() => setShow(false)}
              />
              <div className="w-full text-emphasis-300 text-grayscale-white-90 phone:hidden">
                {numberOfItems} miners selected
              </div>
            </div>
            <div className="w-12 phone:hidden"></div>
            <div className="flex flex-wrap justify-start gap-3">
              {renderActions(numberOfItems, setHidden)}
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default ActionBar;
