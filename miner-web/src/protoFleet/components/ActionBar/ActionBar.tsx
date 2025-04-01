import { useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import PerformanceWidget from "./PerformanceWidget/PerformanceWidget";
import SettingsWidget from "./SettingsWidget";
import DeviceWidget from "@/protoFleet/components/ActionBar/DeviceWidget/DeviceWidget.tsx";
import { DismissTiny } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { sizes } from "@/shared/components/ButtonGroup";

interface ActionBarProps {
  selectedMiners: string[];
}

const ActionBar = ({ selectedMiners }: ActionBarProps) => {
  const [show, setShow] = useState(false);

  useEffect(() => {
    setShow(selectedMiners.length > 0);
  }, [selectedMiners]);

  const [hidden, setHidden] = useState(false);

  const numberOfMiners = useMemo(() => {
    return selectedMiners.length;
  }, [selectedMiners]);

  return (
    <>
      {show && (
        <div
          className={clsx(
            "margin-auto fixed right-0 bottom-4 left-0 z-20 flex justify-center",
            { invisible: hidden },
          )}
          data-testid="action-bar"
        >
          <div className="bg-sufrace-elevated-base/70 flex w-xl items-center justify-between rounded-2xl bg-grayscale-gray-87 p-3 shadow-300 phone:w-[calc(100vw-theme(spacing.4))] phone:rounded-full">
            <div className="flex items-center space-x-2">
              <Button
                className="bg-grayscale-white-10! text-grayscale-white-90!"
                prefixIcon={<DismissTiny />}
                variant={variants.secondary}
                size={sizes.compact}
                testId="close-button"
                onClick={() => setShow(false)}
              />
              <div className="text-emphasis-300 text-grayscale-white-90 phone:hidden">
                {numberOfMiners} miners selected
              </div>
            </div>
            <div className="flex items-center space-x-3">
              <DeviceWidget
                numberOfMiners={numberOfMiners}
                setHidden={setHidden}
              />
              <PerformanceWidget
                numberOfMiners={numberOfMiners}
                setHidden={setHidden}
              />
              <SettingsWidget
                numberOfMiners={numberOfMiners}
                setHidden={setHidden}
              />
            </div>
          </div>
        </div>
      )}
    </>
  );
};

export default ActionBar;
