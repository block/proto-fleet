import { ReactNode, useCallback, useEffect, useState } from "react";

import SelectRowListComponent, { SelectType } from ".";
import { BaseIcon } from "@/shared/stories/icons";

interface IconWrapperProps {
  children: ReactNode;
}

const IconWrapper = ({ children }: IconWrapperProps) => {
  return <div className="bg-core-primary-5 p-[6px] rounded-lg">{children}</div>;
};

interface SelectRowProps {
  disabled: boolean;
  hasPrefixIcon: boolean;
  hasSubtext: boolean;
  type: SelectType;
}

const selectRows = {
  one: "One",
  two: "Two",
} as const;

type SelectRow = (typeof selectRows)[keyof typeof selectRows];

export const SelectRowList = ({
  disabled,
  hasPrefixIcon,
  hasSubtext,
  type,
}: SelectRowProps) => {
  const [selected, setSelected] = useState<SelectRow[]>([selectRows.one]);

  useEffect(() => {
    setSelected([selectRows.one]);
  }, [type]);

  const onChange = useCallback(
    (id: string, isSelected: boolean) => {
      const selectRow = id as SelectRow;
      if (type === "radio") {
        if (isSelected) {
          setSelected([selectRow]);
        }
      } else if (type === "checkbox") {
        if (isSelected && !selected.includes(selectRow)) {
          setSelected([...selected, selectRow]);
        } else if (!isSelected && selected.includes(selectRow)) {
          setSelected(
            selected.filter((selectedRow) => selectedRow !== selectRow),
          );
        }
      }
    },
    [selected, type],
  );

  return (
    <SelectRowListComponent
      className="w-96"
      type={type}
      selectRows={[
        {
          id: selectRows.one,
          isSelected: selected.includes(selectRows.one),
          prefixIcon: hasPrefixIcon && (
            <IconWrapper>
              <BaseIcon />
            </IconWrapper>
          ),
          text: "Select row",
        },
        {
          disabled,
          id: selectRows.two,
          isSelected: selected.includes(selectRows.two),
          prefixIcon: hasPrefixIcon && (
            <IconWrapper>
              <BaseIcon />
            </IconWrapper>
          ),
          subtext: hasSubtext ? "Select row subtitle text." : undefined,
          text: "Select row",
        },
      ]}
      onChange={onChange}
    />
  );
};

export default {
  title: "Components (Shared)/Select Row List",
  component: SelectRowList,
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
      options: ["radio", "checkbox"],
    },
  },
};
