import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { Checkmark, PartialCheckmark } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import { SelectType, selectTypes } from "@/shared/constants";

export interface SelectRowProps {
  className?: string;
  id: string;
  isSelected: boolean;
  partiallySelected?: boolean;
  divider?: boolean;
  onChange: (id: string, isSelected: boolean) => void;
  prefixIcon?: ReactNode;
  text: string | ReactNode;
  sideText?: string | ReactNode;
  type: SelectType;
  "data-testid"?: string;
}

const SelectRow = ({
  className,
  id,
  isSelected,
  partiallySelected,
  onChange,
  divider = true,
  prefixIcon,
  text,
  sideText,
  type,
  "data-testid": dataTestId,
}: SelectRowProps) => {
  const isCheckbox = useMemo(() => type === selectTypes.checkbox, [type]);
  const isRadio = useMemo(() => type === selectTypes.radio, [type]);

  const handleChange = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      onChange(id, e.target.checked);
    },
    [id, onChange],
  );

  return (
    <>
      <div
        className={clsx(
          "flex items-center rounded-xl p-3 text-left select-none",
          "transition-[background-color] duration-200 ease-in-out",
          "cursor-pointer bg-surface-default text-text-primary hover:bg-core-primary-5",
          className,
        )}
        data-testid={dataTestId}
        onClick={() => onChange(id, !isSelected)}
      >
        <div className="flex grow items-center">
          {prefixIcon}
          <div className={clsx({ "ml-4": prefixIcon })}>
            <div className="text-emphasis-300">{text}</div>
          </div>
        </div>
        {sideText ? (
          <div className="ml-4">
            <div className="text-300">{sideText}</div>
          </div>
        ) : null}
        <div className="relative ml-4 flex">
          <input
            className={clsx("peer relative h-[20px] w-[20px] cursor-pointer appearance-none border border-border-20", {
              "rounded-full": isRadio,
              rounded: isCheckbox,
            })}
            type={type}
            checked={isSelected}
            onChange={handleChange}
          />
          <div
            className={clsx("absolute hidden h-full w-full rounded-full bg-core-accent-80 text-text-contrast", {
              "peer-checked:block": isRadio,
            })}
          >
            <svg width="10" height="10" viewBox="0 0 10 10" className="absolute top-[5px] left-[5px]">
              <circle cx="5" cy="5" r="5" fill="currentColor" />
            </svg>
          </div>
          {partiallySelected ? (
            <PartialCheckmark
              className={clsx("absolute block cursor-pointer rounded-sm bg-core-primary-fill/40 text-surface-base")}
            />
          ) : (
            <Checkmark
              className={clsx("absolute hidden cursor-pointer rounded-sm bg-core-accent-80 text-surface-base", {
                "peer-checked:block": isCheckbox,
              })}
            />
          )}
        </div>
      </div>
      {divider ? <Divider className="mt-[-1px] px-4" /> : null}
    </>
  );
};

export default SelectRow;
