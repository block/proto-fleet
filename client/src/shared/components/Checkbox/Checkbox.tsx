import { ChangeEvent } from "react";
import clsx from "clsx";
import { Checkmark, PartialCheckmark } from "@/shared/assets/icons";

type CheckboxProps = {
  onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
  checked?: boolean;
  partiallyChecked?: boolean;
  className?: string;
  disabled?: boolean;
};

const Checkbox = ({ onChange, checked, partiallyChecked = false, className = "", disabled = false }: CheckboxProps) => {
  return (
    <div className={clsx(className, "relative h-[20px] w-[20px]")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={onChange}
        disabled={disabled}
        className={clsx(
          "peer h-full w-full appearance-none rounded-sm border checked:border-transparent checked:bg-core-accent-fill",
          {
            "cursor-pointer border-border-20": !disabled,
            "cursor-not-allowed border-transparent bg-border-20": disabled,
          },
        )}
      />
      {!disabled && (
        <>
          {partiallyChecked ? (
            <PartialCheckmark
              className={clsx(
                "pointer-events-none absolute top-0 block cursor-pointer rounded-sm bg-core-primary-fill/40 text-surface-base",
              )}
            />
          ) : (
            <Checkmark className="pointer-events-none absolute top-0 hidden h-full w-full text-text-contrast peer-checked:block" />
          )}
        </>
      )}
    </div>
  );
};

export default Checkbox;
