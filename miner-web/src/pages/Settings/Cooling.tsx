import { useState } from "react";

import { useCoolingMode, useCoolingStatus } from "api";

import Cooling, { FanMode } from "components/Cooling";
import { ToastType, toastTypes } from "components/Toast";

import StatusToast from "./StatusToast";

const SettingsCooling = () => {
  const [toastType, setToastType] = useState<ToastType | null>(null);

  const { setCoolingMode } = useCoolingMode();
  const { data: coolingStatus } = useCoolingStatus({ poll: true });

  const onChangeFanMode = (fanMode: FanMode, isSelected: boolean) => {
    if (isSelected) {
      setToastType(toastTypes.loading);
      setCoolingMode({
        fanMode,
        onSuccess: () => {
          setToastType(toastTypes.success);
        },
        onError: () => {
          setToastType(toastTypes.error);
        },
      });
    }
  };

  return (
    <>
      <StatusToast onClose={() => setToastType(null)} type={toastType} />
      <Cooling onChange={onChangeFanMode} mode={coolingStatus?.fan_mode} />
    </>
  );
};

export default SettingsCooling;
