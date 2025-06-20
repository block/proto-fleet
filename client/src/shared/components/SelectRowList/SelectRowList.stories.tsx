import { ReactNode, useCallback, useEffect, useState } from "react";
import type { StoryObj } from "@storybook/react";

import SelectRowListComponent from ".";
import { rowListVariants } from "@/shared/components/SelectRowList/constants";
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
  variant: keyof typeof rowListVariants;
}

const selectRows = {
  one: "One",
  two: "Two",
} as const;

type SelectRow = (typeof selectRows)[keyof typeof selectRows];

const SelectRowListForStory = ({
  disabled,
  hasPrefixIcon,
  hasSubtext,
  type,
  variant,
}: SelectRowProps) => {
  const [selected, setSelected] = useState<SelectRow[]>([selectRows.one]);

  useEffect(() => {
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
      variant={variant}
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

export const SelectRowList: StoryObj<typeof SelectRowListForStory> = {};

export default {
  title: "Components (Shared)/Select Row List",
  component: SelectRowListForStory,
  args: {
    disabled: false,
    hasPrefixIcon: true,
    hasSubtext: true,
    type: selectTypes.radio,
    variant: rowListVariants.stack,
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
    variant: {
      control: "select",
      options: Object.values(rowListVariants),
    },
  },
};
