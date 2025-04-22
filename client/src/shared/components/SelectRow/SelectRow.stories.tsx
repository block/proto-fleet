import { ReactNode, useEffect, useState } from "react";

import SelectRowComponent from ".";
import { SelectType, selectTypes } from "@/shared/constants";
import { BaseIcon } from "@/shared/stories/icons";

interface IconWrapperProps {
  children: ReactNode;
}

const IconWrapper = ({ children }: IconWrapperProps) => {
  return <div className="rounded-lg bg-core-primary-5 p-[6px]">{children}</div>;
};

interface SelectRowProps {
  disabled: boolean;
  hasPrefixIcon: boolean;
  hasSubtext: boolean;
  type: SelectType;
}

export const SelectRow = ({ hasPrefixIcon, type }: SelectRowProps) => {
  const [selected, setSelected] = useState<number[]>([]);

  useEffect(() => {
    setSelected([0]);
  }, [type]);

  const handleChange = (id: string, isSelected: boolean) => {
    const index = parseInt(id);
    if (type === selectTypes.checkbox) {
      if (isSelected && !selected.includes(index)) {
        setSelected([...selected, index]);
      } else if (!isSelected && selected.includes(index)) {
        setSelected(
          selected.filter((selectedIndex) => selectedIndex !== index),
        );
      }
    } else {
      setSelected([index]);
    }
  };

  return (
    <div className="flex w-80 flex-col">
      {[...Array(5)].map((_, index) => {
        return (
          <SelectRowComponent
            key={index}
            id={index.toString()}
            text="Select Row"
            isSelected={selected.includes(index)}
            onChange={handleChange}
            prefixIcon={
              hasPrefixIcon ? (
                <IconWrapper>
                  <BaseIcon />
                </IconWrapper>
              ) : null
            }
            type={type}
          />
        );
      })}
    </div>
  );
};

export default {
  title: "Components (Shared)/Select Row",
  args: {
    disabled: false,
    hasPrefixIcon: true,
    hasSubtext: true,
    type: "radio",
  },
  argTypes: {
    disabled: {
      control: "boolean",
    },
    hasPrefixIcon: {
      control: "boolean",
    },
    hasSubtext: {
      control: "boolean",
    },
    type: {
      control: "select",
      options: Object.keys(selectTypes),
    },
  },
};
