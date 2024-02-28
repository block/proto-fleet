import { ReactNode } from "react";

import SelectRowList, { selectTypes } from "components/SelectRowList";

import CoolingIcon from "icons/Cooling";
import FanIcon from "icons/Fan";

import { fanModes } from "../constants";

interface IconWrapperProps {
  children: ReactNode;
}

const IconWrapper = ({ children }: IconWrapperProps) => {
  return <div className="bg-surface-5 p-[6px] rounded-lg">{children}</div>;
};

interface CoolingProps {
  fanMode: string;
  onChange: (id: string, isSelected: boolean) => void;
}

const Cooling = ({ fanMode, onChange }: CoolingProps) => {
  return (
    <SelectRowList
      type={selectTypes.radio}
      selectRows={[
        {
          id: fanModes.auto,
          isSelected: fanMode === fanModes.auto,
          prefixIcon: (
            <IconWrapper>
              <FanIcon />
            </IconWrapper>
          ),
          text: "Fan cooled",
        },
        {
          id: fanModes.false,
          isSelected: fanMode === fanModes.false,
          prefixIcon: (
            <IconWrapper>
              <CoolingIcon />
            </IconWrapper>
          ),
          subtext: "This will disable any connected fans.",
          text: "Immersion cooled",
        },
      ]}
      onChange={onChange}
    />
  );
};

export default Cooling;
