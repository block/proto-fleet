import { ChangeEvent, ReactNode, useCallback, useMemo } from "react";
import clsx from "clsx";

import { SelectType, selectTypes } from ".";
import { Checkmark } from "@/shared/assets/icons";

import Row from "@/shared/components/Row";

export interface SelectRowProps {
  className?: string;
  id: string;
  isSelected: boolean;
  onChange: (id: string, isSelected: boolean) => void;
  prefixIcon?: ReactNode;
  text: string;
  type: SelectType;
}

const SelectRow = ({
  className,
  id,
  isSelected,
  onChange,
  prefixIcon,
  text,
  type,
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
    <Row
      className={clsx(
        "rounded-xl flex items-center select-none text-left p-3",
        "transition-[background-color] ease-in-out duration-200",
        "text-text-primary bg-surface-default hover:bg-core-primary-5 cursor-pointer",
        className,
      )}
      onClick={() => onChange(id, !isSelected)}
    >
      <div className="flex items-center grow">
        {prefixIcon}
        <div className={clsx({ "ml-4": prefixIcon })}>
          <div className="text-emphasis-300">{text}</div>
        </div>
      </div>
      <div className="ml-4 flex relative">
        <input
          className={clsx(
            "peer appearance-none border h-[20px] w-[20px] relative border-border-20 cursor-pointer",
            {
              "rounded-full": isRadio,
              rounded: isCheckbox,
            },
          )}
          type={type}
          checked={isSelected}
          onChange={handleChange}
        />
        <div
          className={clsx(
            "hidden absolute rounded-full w-full h-full bg-core-accent-80 text-text-contrast",
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
            "absolute bg-core-accent-80 rounded-sm text-surface-base hidden cursor-pointer",
            { "peer-checked:block": isCheckbox },
          )}
        />
      </div>
    </Row>
  );
};

export default SelectRow;
