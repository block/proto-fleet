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
    <div className={clsx(className, "relative w-[20px] h-[20px]")}>
      <input
        type="checkbox"
        checked={checked}
        onChange={(e) => {
          if (onChange) onChange(e);
        }}
        className="peer w-full h-full appearance-none appearance-none rounded-sm border border-border-20 checked:bg-core-accent-fill checked:border-transparent"
      />
      <Checkmark className="absolute top-0 hidden peer-checked:block text-text-contrast w-full h-full pointer-events-none" />
    </div>
  );
};

export default Checkbox;
