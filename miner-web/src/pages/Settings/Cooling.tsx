import { useCoolingMode } from "api";

import Cooling, { FanMode } from "components/Cooling";

const SettingsCooling = () => {
  const { setCoolingMode } = useCoolingMode();

  const onChangeFanMode = (fanMode: FanMode, isSelected: boolean) => {
    if (isSelected) {
      setCoolingMode({
        fanMode,
      });
    }
  };

  return <Cooling onChange={onChangeFanMode} />;
};

export default SettingsCooling;
