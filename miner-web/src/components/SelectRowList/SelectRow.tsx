import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { Checkmark } from "icons";

import { SelectType, selectTypes } from ".";

export interface SelectRowProps {
  isSelected: boolean;
  onChange: (isSelected: boolean) => void;
  prefixIcon?: ReactNode;
  subtext?: string;
  text: string;
  type: SelectType;
}

const SelectRow = ({
  isSelected,
  onChange,
  prefixIcon,
  subtext,
  text,
  type,
}: SelectRowProps) => {
  const isCheckbox = useMemo(() => type === selectTypes.checkbox, [type]);
  const isRadio = useMemo(() => type === selectTypes.radio, [type]);

  const handleChange = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      onChange(e.target.checked);
    },
    [onChange]
  );

  return (
    <div
      className={clsx(
        "rounded-xl text-text-primary bg-surface-default flex items-center cursor-pointer select-none",
        "hover:bg-surface-5 transition-[background-color] ease-in-out duration-200",
        {
          "border-[1.5px] border-border-primary p-4": isSelected,
          "border border-border-primary/5 p-[16.5px]": !isSelected,
        }
      )}
      onClick={() => onChange(!isSelected)}
    >
      <div className="flex items-center grow">
        {prefixIcon}
        <div className={clsx({ "ml-4": prefixIcon })}>
          <div className="text-emphasis-300">{text}</div>
          {subtext && (
            <div className="text-200 text-text-primary/70">{subtext}</div>
          )}
        </div>
      </div>
      <div className="ml-4 flex relative">
        <input
          className={clsx(
            "peer appearance-none border border-border-primary/20 h-[20px] w-[20px] relative cursor-pointer",
            {
              "rounded-full": isRadio,
              rounded: isCheckbox,
            }
          )}
          type={type}
          checked={isSelected}
          onChange={handleChange}
        />
        <div
          className={clsx(
            "hidden absolute rounded-full w-full h-full bg-core-accent-fill/80 text-text-contrast",
            {
              "peer-checked:block": isRadio,
            }
          )}
        >
          <svg
            width="10"
            height="10"
            viewBox="0 0 10 10"
            className="absolute top-[5px] left-[5px]"
          >
            <circle cx="5" cy="5" r="5" fill="currentColor" />
          </svg>
        </div>
        <Checkmark
          className={clsx(
            "absolute bg-core-accent-fill/80 rounded text-surface-base hidden cursor-pointer",
            { "peer-checked:block": isCheckbox }
          )}
        />
      </div>
    </div>
  );
};

export default SelectRow;
