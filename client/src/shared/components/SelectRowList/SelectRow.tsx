import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { Checkmark } from "@/shared/assets/icons";
import { SelectType, selectTypes } from "@/shared/constants";

export interface SelectRowProps {
  className?: string;
  disabled?: boolean;
  isSelected: boolean;
  onChange: (isSelected: boolean) => void;
  prefixIcon?: ReactNode;
  subtext?: string;
  text: string;
  sideText?: string;
  type: SelectType;
}

const SelectRow = ({
  className,
  disabled,
  isSelected,
  onChange,
  prefixIcon,
  subtext,
  text,
  sideText,
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
        "flex w-full items-center justify-between py-3 text-left select-none",
        "transition-[background-color] duration-200 ease-in-out",
        "h-12 border-none",
        {
          "cursor-pointer text-text-primary": !disabled,
          "cursor-not-allowed bg-core-primary-5 text-text-primary-50": disabled,
        },
        className,
      )}
      disabled={disabled}
      onClick={() => onChange(!isSelected)}
    >
      <div className="flex items-center">
        {prefixIcon}
        <div className={clsx({ "ml-4": prefixIcon })}>
          <div className="text-emphasis-300">{text}</div>
          {subtext ? (
            <div
              className={clsx("text-200", {
                "text-text-primary-70": !disabled,
                "text-text-primary-50": disabled,
              })}
            >
              {subtext}
            </div>
          ) : null}
        </div>
      </div>
      <div className="flex items-center gap-4">
        {sideText ? <div className="text-right text-300">{sideText}</div> : null}
        <div className="relative flex">
          <input
            className={clsx("peer relative h-[20px] w-[20px] appearance-none", {
              "rounded-full": isRadio,
              rounded: isCheckbox,
              "cursor-pointer": !disabled,
              "cursor-not-allowed opacity-[0.4]": disabled,
              "border border-border-20": isRadio && !isSelected,
              "bg-core-accent-fill": isRadio && isSelected && !disabled,
            })}
            disabled={disabled}
            type={type}
            checked={isSelected ? !disabled : false}
            onChange={handleChange}
          />
          <div
            className={clsx("absolute top-[5px] left-[5px] hidden h-[10px] w-[10px] rounded-full bg-white shadow-sm", {
              "peer-checked:block": isRadio,
            })}
          />
          <Checkmark
            className={clsx("absolute hidden cursor-pointer rounded-sm bg-core-accent-80 text-surface-base", {
              "peer-checked:block": isCheckbox,
            })}
          />
        </div>
      </div>
    </button>
  );
};

export default SelectRow;
