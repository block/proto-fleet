import { ReactNode, useCallback, useEffect, useState } from "react";

import Cooling from "icons/Cooling";
import Fan from "icons/Fan";

import SelectRowListComponent, { SelectType } from ".";

interface IconWrapperProps {
  children: ReactNode;
}

const IconWrapper = ({ children }: IconWrapperProps) => {
  return <div className="bg-surface-5 p-[6px] rounded-lg">{children}</div>;
};

interface SelectRowProps {
  hasPrefixIcon: boolean;
  hasSubtext: boolean;
  type: SelectType;
}

const fanModes = {
  auto: "auto",
  false: "false",
} as const;

type FanMode = keyof typeof fanModes;

export const SelectRowList = ({
  hasPrefixIcon,
  hasSubtext,
  type,
}: SelectRowProps) => {
  const [selected, setSelected] = useState<FanMode[]>([fanModes.auto]);

  useEffect(() => {
    setSelected([fanModes.auto]);
  }, [type]);

  const onChange = useCallback(
    (id: string, isSelected: boolean) => {
      const fanMode = id as FanMode;
      if (type === "radio") {
        if (isSelected) {
          setSelected([fanMode]);
        }
      } else if (type === "checkbox") {
        if (isSelected && !selected.includes(fanMode)) {
          setSelected([...selected, fanMode]);
        } else if (!isSelected && selected.includes(fanMode)) {
          setSelected(
            selected.filter((selectedFanMode) => selectedFanMode !== fanMode)
          );
        }
      }
    },
    [selected, type]
  );

  return (
    <SelectRowListComponent
      className="w-96"
      type={type}
      selectRows={[
        {
          id: fanModes.auto,
          isSelected: selected.includes(fanModes.auto),
          prefixIcon: hasPrefixIcon && (
            <IconWrapper>
              <Fan />
            </IconWrapper>
          ),
          text: "Fan cooled",
        },
        {
          id: fanModes.false,
          isSelected: selected.includes(fanModes.false),
          prefixIcon: hasPrefixIcon && (
            <IconWrapper>
              <Cooling />
            </IconWrapper>
          ),
          subtext: hasSubtext
            ? "This will disable any connected fans."
            : undefined,
          text: "Immersion cooled",
        },
      ]}
      onChange={onChange}
    />
  );
};

export default {
  title: "Select Row List",
  component: SelectRowList,
  args: {
    hasPrefixIcon: true,
    hasSubtext: true,
    type: "radio",
  },
  argTypes: {
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
