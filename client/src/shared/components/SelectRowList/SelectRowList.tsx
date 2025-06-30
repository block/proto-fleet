import clsx from "clsx";

import SelectRow, { SelectRowProps } from "./SelectRow";
import { rowListVariants } from "@/shared/components/SelectRowList/constants";
import { SelectType } from "@/shared/constants";

interface SelectRows extends Omit<SelectRowProps, "onChange" | "type"> {
  id: string;
}

interface SelectRowListProps {
  className?: string;
  onChange: (id: string, isSelected: boolean) => void;
  selectRows: SelectRows[];
  type: SelectType;
  variant: keyof typeof rowListVariants;
}

const SelectRowList = ({
  className,
  onChange,
  selectRows,
  type,
  variant,
}: SelectRowListProps) => {
  const horizontalGap = "space-x-4";
  const verticalGap = "space-y-4";
  const classes = ["flex"];

  switch (variant) {
    case rowListVariants.stack:
      classes.push(...["flex-col", verticalGap]);
      break;
    case rowListVariants.fill:
      classes.push(...["w-full", horizontalGap]);
      break;
  }

  return (
    <div className={clsx(classes, className)}>
      {selectRows.map((selectRow) => {
        const handleChange = (isSelected: boolean) => {
          onChange(selectRow.id, isSelected);
        };

        return (
          <SelectRow
            key={selectRow.id}
            className={clsx({ grow: variant === rowListVariants.fill })}
            subtext={selectRow.subtext}
            text={selectRow.text}
            sideText={selectRow.sideText}
            disabled={selectRow.disabled}
            isSelected={selectRow.isSelected}
            onChange={handleChange}
            prefixIcon={selectRow.prefixIcon}
            type={type}
          />
        );
      })}
    </div>
  );
};

export default SelectRowList;
