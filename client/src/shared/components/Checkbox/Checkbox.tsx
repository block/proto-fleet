import { type ChangeEvent, type ReactElement } from "react";
import clsx from "clsx";
import { Checkmark, PartialCheckmark } from "@/shared/assets/icons";

type CheckboxProps = {
  onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
  checked?: boolean;
  partiallyChecked?: boolean;
  className?: string;
  disabled?: boolean;
};

function Checkbox({
  onChange,
  checked,
  partiallyChecked = false,
  className = "",
  disabled = false,
}: CheckboxProps): ReactElement {
  return (
    <div className={clsx(className, "relative h-[20px] w-[20px]")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={onChange}
        disabled={disabled}
        className={clsx("peer h-full w-full appearance-none rounded-sm border", {
          "cursor-pointer border-border-20 checked:border-transparent checked:bg-core-accent-fill": !disabled,
          "cursor-not-allowed border-transparent bg-border-20 checked:bg-border-20": disabled,
        })}
      />
      {partiallyChecked ? (
        <PartialCheckmark
          className={clsx("pointer-events-none absolute top-0 block rounded-sm", {
            "cursor-pointer bg-core-primary-fill/40 text-surface-base": !disabled,
            "cursor-not-allowed bg-border-20 text-text-primary-50": disabled,
          })}
        />
      ) : (
        <Checkmark
          className={clsx("pointer-events-none absolute top-0 hidden h-full w-full peer-checked:block", {
            "text-text-contrast": !disabled,
            "text-text-primary-50": disabled,
          })}
        />
      )}
    </div>
  );
}

export default Checkbox;
