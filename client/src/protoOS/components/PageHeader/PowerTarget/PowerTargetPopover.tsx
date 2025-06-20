import { useCallback, useEffect, useRef, useState } from "react";
import { useMiningTarget } from "@/protoOS/api";
import {
  PerformanceMode,
  performanceModes,
} from "@/protoOS/components/PageHeader/PowerTarget/constants";
import { Efficiency, Hashrate } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Popover from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SelectRowList from "@/shared/components/SelectRowList";
import { rowListVariants } from "@/shared/components/SelectRowList/constants";
import { positions, selectTypes } from "@/shared/constants";
import { convertWtoKW } from "@/shared/utils/utility";

export type PowerTargetPopoverProps = {
  onDismiss: () => void;
};

const PowerTargetPopover = ({ onDismiss }: PowerTargetPopoverProps) => {
  const { miningTarget, performanceMode, bounds, pending, updateMiningTarget } =
    useMiningTarget();
  const [selectedPerformanceMode, setSelectedPerformanceMode] = useState<
    PerformanceMode | undefined
  >(performanceMode);
  const [inputValue, setInputValue] = useState<string>();
  const [error, setError] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null);

  const onChange = (value: string) => {
    const parsedValue = parseFloat(value as string);
    if (isNaN(parsedValue)) {
      return;
    }

    if (
      bounds &&
      (parsedValue < convertWtoKW(bounds.min) ||
        parsedValue > convertWtoKW(bounds.max))
    ) {
      setError(
        `Value must be between ${convertWtoKW(bounds.min)}kW and ${convertWtoKW(bounds.max)}kW`,
      );
    } else {
      setError(undefined);
    }
  };

  useEffect(() => {
    setSelectedPerformanceMode(performanceMode);
  }, [pending, performanceMode]);

  useEffect(() => {
    if (pending || miningTarget === undefined) {
      setInputValue(undefined);
      return;
    }

    setInputValue(`${convertWtoKW(miningTarget)}`);
  }, [pending, miningTarget]);

  const handleUpdate = useCallback(() => {
    if (inputRef.current === null || pending) {
      return;
    }

    updateMiningTarget({
      performance_mode: selectedPerformanceMode,
      power_target_watts: +inputRef.current.value * 1000,
    });
  }, [pending, selectedPerformanceMode, updateMiningTarget]);

  return (
    <Popover position={positions["bottom left"]} className="w-102">
      <div>
        <h2 className="text-heading-100 text-text-primary">
          Performance target
        </h2>
        <p className="text-300 text-text-primary-70">
          Prioritize hashrate or efficiency.
        </p>
      </div>

      <SelectRowList
        type={selectTypes.radio}
        variant={rowListVariants.fill}
        selectRows={[
          {
            id: performanceModes.MaximumHashrate,
            isSelected:
              selectedPerformanceMode === performanceModes.MaximumHashrate,
            prefixIcon: <Hashrate />,
            text: "Hashrate",
          },
          {
            id: performanceModes.Efficiency,
            isSelected: selectedPerformanceMode === performanceModes.Efficiency,
            prefixIcon: <Efficiency />,
            text: "Efficiency",
          },
        ]}
        onChange={(id, isSelected) => {
          if (isSelected) setSelectedPerformanceMode(id as PerformanceMode);
        }}
      />

      <Input
        id={"power-target-input"}
        label="Power target"
        className="w-full"
        initValue={inputValue}
        type="number"
        inputRef={inputRef}
        onChange={onChange}
        error={error}
        units={"kW"}
      />

      <div className={"grid grid-cols-2 gap-2"}>
        <Button
          text="Cancel"
          variant={variants.secondary}
          className="grow"
          size={sizes.compact}
          onClick={onDismiss}
        />
        <Button
          text={pending ? "Applying" : "Apply"}
          variant={variants.primary}
          size={sizes.compact}
          disabled={!pending && !!error}
          prefixIcon={
            pending ? <ProgressCircular indeterminate size={12} /> : undefined
          }
          testId="power-target-apply-button"
          onClick={() => {
            if (pending || inputRef.current === null) {
              return;
            }

            handleUpdate();
          }}
        />
      </div>
    </Popover>
  );
};

export default PowerTargetPopover;
