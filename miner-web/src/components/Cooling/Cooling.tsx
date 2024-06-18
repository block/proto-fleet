import { ReactNode, useEffect, useState } from "react";

import ContentHeader from "components/ContentHeader";
import SelectRowList, { selectTypes } from "components/SelectRowList";

import { Fan, ImmersionCooling } from "icons";

import { fanModes } from "./constants";
import { FanMode } from "./types";

interface IconWrapperProps {
  children: ReactNode;
}

const IconWrapper = ({ children }: IconWrapperProps) => {
  return <div className="bg-surface-5 p-[6px] rounded-lg">{children}</div>;
};

interface CoolingProps {
  mode?: FanMode;
  onChange: (fanMode: FanMode, isSelected: boolean) => void;
}

const Cooling = ({ mode, onChange }: CoolingProps) => {
  const [fanMode, setFanMode] = useState<FanMode | undefined>(
    mode && Object.values(fanModes).includes(mode) ? (mode as FanMode) : undefined
  );

  useEffect(() => {
    if (mode && mode in fanModes) {
      setFanMode(mode as FanMode);
    }
  }, [mode]);

  const handleChange = (id: string, isSelected: boolean) => {
    if (isSelected) {
      const newFanMode = id as FanMode;
      setFanMode(newFanMode);
      onChange(newFanMode, isSelected);
    }
  };

  return (
    <div className="max-w-[640px]">
      <ContentHeader
        title="Cooling"
        subtitle="Choose how you want to cool your device. This can be changed at any time."
        testId="cooling-title"
      />
      <SelectRowList
        type={selectTypes.radio}
        selectRows={[
          {
            id: fanModes.auto,
            isSelected: fanMode === fanModes.auto,
            prefixIcon: (
              <IconWrapper>
                <Fan />
              </IconWrapper>
            ),
            text: "Fan cooled",
          },
          {
            id: fanModes.false,
            isSelected: fanMode === fanModes.false,
            prefixIcon: (
              <IconWrapper>
                <ImmersionCooling />
              </IconWrapper>
            ),
            subtext: "This will disable any connected fans.",
            text: "Immersion cooled",
          },
        ]}
        onChange={handleChange}
      />
    </div>
  );
};

export default Cooling;
