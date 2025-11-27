import { ChangeEvent } from "react";
import clsx from "clsx";

type RadioProps = {
  onChange?: (e: ChangeEvent<HTMLInputElement>) => void;
  selected?: boolean;
  className?: string;
};

const Radio = ({ onChange, selected, className = "" }: RadioProps) => {
  return (
    <div className={clsx(className, "relative flex cursor-pointer")}>
      <input
        type="radio"
        checked={selected}
        onChange={(e) => {
          if (onChange) onChange(e);
        }}
        className="peer relative h-[20px] w-[20px] cursor-pointer appearance-none rounded-full border border-border-20"
      />
      <div className="absolute hidden h-[20px] w-[20px] rounded-full bg-core-accent-80 text-text-contrast peer-checked:block">
        <svg width="10" height="10" viewBox="0 0 10 10" className="absolute top-[5px] left-[5px]">
          <circle cx="5" cy="5" r="5" fill="currentColor" />
        </svg>
      </div>
    </div>
  );
};

export default Radio;
