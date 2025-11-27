import clsx from "clsx";

import SelectRow, { SelectRowProps } from "./SelectRow";
import { SelectType } from "@/shared/constants";

interface SelectRows extends Omit<SelectRowProps, "onChange" | "type"> {
  id: string;
}

interface SelectRowListProps {
  className?: string;
  onChange: (id: string, isSelected: boolean) => void;
  selectRows: SelectRows[];
  type: SelectType;
}

const SelectRowList = ({ className, onChange, selectRows, type }: SelectRowListProps) => {
  return (
    <div className={clsx(className)}>
      {selectRows.map((selectRow, index) => {
        const handleChange = (isSelected: boolean) => {
          onChange(selectRow.id, isSelected);
        };

        return (
          <div key={selectRow.id}>
            <SelectRow
              subtext={selectRow.subtext}
              text={selectRow.text}
              sideText={selectRow.sideText}
              disabled={selectRow.disabled}
              isSelected={selectRow.isSelected}
              onChange={handleChange}
              prefixIcon={selectRow.prefixIcon}
              type={type}
            />
            {/* Add divider line after each row except the last */}
            {index < selectRows.length - 1 && <div className="h-[0.5px] bg-border-10" />}
          </div>
        );
      })}
    </div>
  );
};

export default SelectRowList;
