import { ChangeEvent } from "react";
import clsx from "clsx";
import { Checkmark, PartialCheckmark } from "@/shared/assets/icons";

type CheckboxProps = {
  onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
  checked?: boolean;
  partiallyChecked?: boolean;
  className?: string;
};

const Checkbox = ({
  onChange,
  checked,
  partiallyChecked = false,
  className = "",
}: CheckboxProps) => {
  return (
    <div className={clsx(className, "relative h-[20px] w-[20px]")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => {
          if (onChange) onChange(e);
        }}
        className="peer h-full w-full cursor-pointer appearance-none rounded-sm border border-border-20 checked:border-transparent checked:bg-core-accent-fill"
      />
      {partiallyChecked ? (
        <PartialCheckmark
          className={clsx(
            "pointer-events-none absolute top-0 block cursor-pointer rounded-sm bg-core-primary-fill/40 text-surface-base",
          )}
        />
      ) : (
        <Checkmark className="pointer-events-none absolute top-0 hidden h-full w-full text-text-contrast peer-checked:block" />
      )}
    </div>
  );
};

export default Checkbox;
