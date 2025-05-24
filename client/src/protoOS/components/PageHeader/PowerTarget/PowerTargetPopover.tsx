import { useEffect, useRef, useState } from "react";
import { useMiningTarget } from "@/protoOS/api";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Popover from "@/shared/components/Popover";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { positions } from "@/shared/constants";
import { convertWtoKW } from "@/shared/utils/utility";

export type PowerTargetPopoverProps = {
  onDismiss: () => void;
};

const PowerTargetPopover = ({ onDismiss }: PowerTargetPopoverProps) => {
  const { miningTarget, bounds, pending, updateMiningTarget } =
    useMiningTarget();
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
    if (pending || miningTarget === undefined) {
      setInputValue(undefined);
      return;
    }

    setInputValue(`${convertWtoKW(miningTarget)}`);
  }, [pending, miningTarget]);

  return (
    <Popover position={positions["bottom left"]}>
      <div>
        <h2 className="text-heading-100 text-text-primary">Power target</h2>
        <p className="text-300 text-text-primary-70">
          Set a power target for the miner.
        </p>
      </div>

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
          onClick={() => {
            if (pending || inputRef.current === null) {
              return;
            }

            updateMiningTarget(+inputRef.current.value * 1000);
          }}
        />
      </div>
    </Popover>
  );
};

export default PowerTargetPopover;
