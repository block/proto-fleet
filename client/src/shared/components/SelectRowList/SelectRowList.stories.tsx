import { ReactNode, useCallback, useEffect, useState } from "react";
import type { StoryObj } from "@storybook/react-vite";

import SelectRowListComponent from ".";
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

const selectRows = {
  one: "One",
  two: "Two",
} as const;

type SelectRow = (typeof selectRows)[keyof typeof selectRows];

const SelectRowListForStory = ({ disabled, hasPrefixIcon, hasSubtext, type }: SelectRowProps) => {
  const [selected, setSelected] = useState<SelectRow[]>([selectRows.one]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset selection when story control "type" changes
    setSelected([selectRows.one]);
  }, [type]);

  const onChange = useCallback(
    (id: string, isSelected: boolean) => {
      const selectRow = id as SelectRow;
      if (type === selectTypes.radio) {
        if (isSelected) {
          setSelected([selectRow]);
        }
      } else if (type === selectTypes.checkbox) {
        if (isSelected && !selected.includes(selectRow)) {
          setSelected([...selected, selectRow]);
        } else if (!isSelected && selected.includes(selectRow)) {
          setSelected(selected.filter((selectedRow) => selectedRow !== selectRow));
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
          prefixIcon: hasPrefixIcon ? (
            <IconWrapper>
              <BaseIcon />
            </IconWrapper>
          ) : null,
          text: "Select row",
        },
        {
          disabled,
          id: selectRows.two,
          isSelected: selected.includes(selectRows.two),
          prefixIcon: hasPrefixIcon ? (
            <IconWrapper>
              <BaseIcon />
            </IconWrapper>
          ) : null,
          subtext: hasSubtext ? "Select row subtitle text." : undefined,
          text: "Select row",
        },
      ]}
      onChange={onChange}
    />
  );
};

export const SelectRowList: StoryObj<typeof SelectRowListForStory> = {};

export default {
  title: "Shared/Select Row List",
  component: SelectRowListForStory,
  args: {
    disabled: false,
    hasPrefixIcon: true,
    hasSubtext: true,
    type: selectTypes.radio,
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
      options: Object.values(selectTypes),
    },
  },
};
