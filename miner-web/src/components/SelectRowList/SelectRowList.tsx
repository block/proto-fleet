import clsx from "clsx";

import SelectRow, { SelectRowProps } from "./SelectRow";
import { SelectType } from ".";

interface SelectRows extends Omit<SelectRowProps, "onChange" | "type"> {
  id: string;
}

interface SelectRowListProps {
  className?: string;
  onChange: (id: string, isSelected: boolean) => void;
  selectRows: SelectRows[];
  type: SelectType;
}

const SelectRowList = ({
  className,
  onChange,
  selectRows,
  type,
}: SelectRowListProps) => {
  return (
    <div className={clsx("flex flex-col space-y-4", className)}>
      {selectRows.map((selectRow, index) => {
        const handleChange = (isSelected: boolean) => {
          onChange(selectRow.id, isSelected);
        };

        return (
          <SelectRow
            key={index}
            subtext={selectRow.subtext}
            text={selectRow.text}
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
