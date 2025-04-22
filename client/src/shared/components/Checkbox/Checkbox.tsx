import { ChangeEvent } from "react";
import clsx from "clsx";
import { Checkmark } from "@/shared/assets/icons";

type CheckboxProps = {
  onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
  checked?: boolean;
  className?: string;
};

const Checkbox = ({ onChange, checked, className = "" }: CheckboxProps) => {
  return (
    <div className={clsx(className, "relative h-[20px] w-[20px]")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => {
          if (onChange) onChange(e);
        }}
        className="peer h-full w-full appearance-none rounded-sm border border-border-20 checked:border-transparent checked:bg-core-accent-fill"
      />
      <Checkmark className="pointer-events-none absolute top-0 hidden h-full w-full text-text-contrast peer-checked:block" />
    </div>
  );
};

export default Checkbox;
