import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { Checkmark } from "icons";

import { SelectType, selectTypes } from ".";

export interface SelectRowProps {
  disabled?: boolean;
  isSelected: boolean;
  onChange: (isSelected: boolean) => void;
  prefixIcon?: ReactNode;
  subtext?: string;
  text: string;
  type: SelectType;
}

const SelectRow = ({
  disabled,
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
    <button
      className={clsx(
        "rounded-xl flex items-center select-none text-left",
        "transition-[background-color] ease-in-out duration-200",
        {
          "border-[1.5px] border-border-primary p-4": isSelected && !disabled,
          "border border-border-5 p-[16.5px]": !isSelected || disabled,
          "text-text-primary bg-surface-default hover:bg-core-primary-5 cursor-pointer":
            !disabled,
          "text-text-primary-50 bg-core-primary-5 cursor-not-allowed": disabled,
        }
      )}
      disabled={disabled}
      onClick={() => onChange(!isSelected)}
    >
      <div className="flex items-center grow">
        {prefixIcon}
        <div className={clsx({ "ml-4": prefixIcon })}>
          <div className="text-emphasis-300">{text}</div>
          {subtext && (
            <div
              className={clsx("text-200", {
                "text-text-primary-70": !disabled,
                "text-text-primary-50": disabled,
              })}
            >
              {subtext}
            </div>
          )}
        </div>
      </div>
      <div className="ml-4 flex relative">
        <input
          className={clsx(
            "peer appearance-none border h-[20px] w-[20px] relative",
            {
              "rounded-full": isRadio,
              rounded: isCheckbox,
              "border-border-20 cursor-pointer": !disabled,
              "border-border-10 cursor-not-allowed bg-core-primary-20 opacity-[0.4]":
                disabled,
            }
          )}
          disabled={disabled}
          type={type}
          checked={isSelected && !disabled}
          onChange={handleChange}
        />
        <div
          className={clsx(
            "hidden absolute rounded-full w-full h-full bg-core-accent-80 text-text-contrast",
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
            "absolute bg-core-accent-80 rounded text-surface-base hidden cursor-pointer",
            { "peer-checked:block": isCheckbox }
          )}
        />
      </div>
    </button>
  );
};

export default SelectRow;
