import { RefObject, useCallback, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { PerformanceMode } from "@/protoOS/api/generatedApi";
import { useMiningTarget } from "@/protoOS/api/hooks/useMiningTarget";
import { PowerTargetMode, powerTargetModes } from "@/protoOS/components/PageHeader/PowerTarget/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Popover from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SelectRowList from "@/shared/components/SelectRowList";
import { positions, selectTypes } from "@/shared/constants";
import { convertWtoKW } from "@/shared/utils/utility";

export type PowerTargetPopoverProps = {
  onDismiss: () => void;
  onUpdateStart?: (miningTarget: { performance_mode?: PerformanceMode; power_target_watts?: number }) => void;
};

const getInitialPowerTargetMode = (
  miningTarget: number | undefined,
  defaultTarget: number | undefined,
  maxTarget: number | undefined,
): PowerTargetMode => {
  if (miningTarget === defaultTarget) {
    return powerTargetModes.default;
  } else if (miningTarget === maxTarget) {
    return powerTargetModes.max;
  } else {
    return powerTargetModes.custom;
  }
};

const PowerTargetPopover = ({ onDismiss, onUpdateStart }: PowerTargetPopoverProps) => {
  const { miningTarget, defaultTarget, performanceMode, bounds, pending } = useMiningTarget();
  const [selectedPerformanceMode, setSelectedPerformanceMode] = useState<PerformanceMode | undefined>(performanceMode);
  const [selectedPowerTargetMode, setSelectedPowerTargetMode] = useState<PowerTargetMode | undefined>(
    getInitialPowerTargetMode(miningTarget, defaultTarget, bounds?.max),
  );

  // Derive inputValue from miningTarget instead of storing in state
  const inputValue = useMemo(
    () => (miningTarget === undefined ? undefined : `${convertWtoKW(miningTarget)}`),
    [miningTarget],
  );

  const [error, setError] = useState<string>();
  const inputRef = useRef<HTMLInputElement>(null) as RefObject<HTMLInputElement>;

  const onChange = (value: string) => {
    const parsedValue = parseFloat(value as string);
    if (isNaN(parsedValue)) {
      return;
    }

    if (bounds) {
      const minKW = convertWtoKW(bounds.min);
      const maxKW = convertWtoKW(bounds.max);

      if (parsedValue < minKW) {
        setError(`This miner has a ${minKW} kW minimum power target.`);
      } else if (parsedValue > maxKW) {
        setError(`This miner has a ${maxKW} kW maximum power target.`);
      } else {
        setError(undefined);
      }
    } else {
      setError(undefined);
    }
  };

  // Sync local draft with performanceMode when it (or pending) changes
  const [prevPerformanceMode, setPrevPerformanceMode] = useState(performanceMode);
  const [prevPending, setPrevPending] = useState(pending);
  if (prevPerformanceMode !== performanceMode || prevPending !== pending) {
    setPrevPerformanceMode(performanceMode);
    setPrevPending(pending);
    setSelectedPerformanceMode(performanceMode);
  }

  // Converts the selected power target to whole watts (the API rejects fractional watts).
  const calculatePowerTargetWattage = useCallback((): number | undefined => {
    if (selectedPowerTargetMode === powerTargetModes.default) {
      return defaultTarget;
    } else if (selectedPowerTargetMode === powerTargetModes.max) {
      return bounds?.max;
    } else if (selectedPowerTargetMode === powerTargetModes.custom && inputRef.current) {
      return Math.round(+inputRef.current.value * 1000);
    } else {
      return defaultTarget;
    }
  }, [selectedPowerTargetMode, defaultTarget, bounds?.max]);

  const handleUpdate = useCallback(() => {
    if (pending || (selectedPowerTargetMode === powerTargetModes.custom && inputRef.current === null)) {
      return;
    }

    const powerTarget = calculatePowerTargetWattage();
    const miningTargetUpdate = {
      performance_mode: selectedPerformanceMode,
      power_target_watts: powerTarget,
    };

    onUpdateStart?.(miningTargetUpdate);
  }, [pending, selectedPerformanceMode, selectedPowerTargetMode, calculatePowerTargetWattage, onUpdateStart]);

  return (
    <Popover position={positions["bottom left"]} className="flex w-80 flex-col gap-4 !space-y-1 p-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-heading-100 text-text-primary">Power target</h2>
        <p className="text-300 text-text-primary-70">Set a power target for the miner.</p>
      </div>
      <SelectRowList
        type={selectTypes.radio}
        selectRows={[
          {
            id: powerTargetModes.default,
            isSelected: selectedPowerTargetMode === powerTargetModes.default,
            text: "Default",
            sideText: `${convertWtoKW(defaultTarget)} kW`,
          },
          {
            id: powerTargetModes.max,
            isSelected: selectedPowerTargetMode === powerTargetModes.max,
            text: "Max",
            sideText: `${convertWtoKW(bounds?.max)} kW`,
          },
          {
            id: powerTargetModes.custom,
            isSelected: selectedPowerTargetMode === powerTargetModes.custom,
            text: "Custom",
          },
        ]}
        onChange={(id, isSelected) => {
          if (isSelected) setSelectedPowerTargetMode(id as PowerTargetMode);
        }}
      />

      {selectedPowerTargetMode === powerTargetModes.custom && (
        <div className="space-y-2">
          <Input
            id={"power-target-input"}
            label="Power target"
            className="w-full"
            initValue={inputValue}
            type="number"
            inputRef={inputRef}
            onChange={onChange}
            units={"kW"}
            disabled={pending}
          />
          <p className={clsx("text-200", error ? "text-intent-critical-fill" : "text-text-primary-70")}>
            {error ||
              `Set this miner's power target between
              ${convertWtoKW(bounds?.min || 0)} kW and ${convertWtoKW(bounds?.max || 0)} kW.`}
          </p>
        </div>
      )}

      <div className="flex gap-3">
        <Button text="Cancel" variant={variants.secondary} className="grow" size={sizes.base} onClick={onDismiss} />
        <Button
          text={pending ? "Applying" : "Apply"}
          variant={variants.primary}
          className="grow"
          size={sizes.base}
          disabled={pending || (!!error && selectedPowerTargetMode === powerTargetModes.custom)}
          prefixIcon={pending ? <ProgressCircular indeterminate size={12} /> : undefined}
          testId="power-target-apply-button"
          onClick={handleUpdate}
        />
      </div>
    </Popover>
  );
};

export default PowerTargetPopover;
