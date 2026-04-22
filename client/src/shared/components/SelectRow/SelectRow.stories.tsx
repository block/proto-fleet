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
  hasPrefixIcon: boolean;
  hasSideText: boolean;
  type: SelectType;
}

export const SelectRow = ({ hasPrefixIcon, hasSideText, type }: SelectRowProps) => {
  const [selected, setSelected] = useState<number[]>([]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSelected([0]);
  }, [type]);

  const handleChange = (id: string, isSelected: boolean) => {
    const index = parseInt(id);
    if (type === selectTypes.checkbox) {
      if (isSelected && !selected.includes(index)) {
        setSelected([...selected, index]);
      } else if (!isSelected && selected.includes(index)) {
        setSelected(selected.filter((selectedIndex) => selectedIndex !== index));
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
            sideText={hasSideText ? "Side Text" : undefined}
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
  title: "Shared/Select Row",
  args: {
    hasPrefixIcon: true,
    hasSideText: true,
    type: "radio",
  },
  argTypes: {
    hasPrefixIcon: {
      control: "boolean",
    },
    hasSideText: {
      control: "boolean",
    },
    type: {
      control: "select",
      options: Object.keys(selectTypes),
    },
  },
};
