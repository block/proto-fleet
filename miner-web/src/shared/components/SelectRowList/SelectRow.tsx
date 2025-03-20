import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { SelectType, selectTypes } from ".";
import { Checkmark } from "@/shared/assets/icons";

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
    [onChange],
  );

  return (
    <button
      className={clsx(
        "flex items-center rounded-xl text-left select-none",
        "transition-[background-color] duration-200 ease-in-out",
        {
          "border-[1.5px] border-border-primary p-4": isSelected && !disabled,
          "border border-border-5 p-[16.5px]": !isSelected || disabled,
          "cursor-pointer bg-surface-default text-text-primary hover:bg-core-primary-5":
            !disabled,
          "cursor-not-allowed bg-core-primary-5 text-text-primary-50": disabled,
        },
      )}
      disabled={disabled}
      onClick={() => onChange(!isSelected)}
    >
      <div className="flex grow items-center">
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
      <div className="relative ml-4 flex">
        <input
          className={clsx(
            "peer relative h-[20px] w-[20px] appearance-none border",
            {
              "rounded-full": isRadio,
              rounded: isCheckbox,
              "cursor-pointer border-border-20": !disabled,
              "cursor-not-allowed border-border-10 bg-core-primary-20 opacity-[0.4]":
                disabled,
            },
          )}
          disabled={disabled}
          type={type}
          checked={isSelected && !disabled}
          onChange={handleChange}
        />
        <div
          className={clsx(
            "absolute hidden h-full w-full rounded-full bg-core-accent-80 text-text-contrast",
            {
              "peer-checked:block": isRadio,
            },
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
            "absolute hidden cursor-pointer rounded-sm bg-core-accent-80 text-surface-base",
            { "peer-checked:block": isCheckbox },
          )}
        />
      </div>
    </button>
  );
};

export default SelectRow;
