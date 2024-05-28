import { useState } from "react";

import { useCoolingMode } from "api";

import Cooling, { FanMode } from "components/Cooling";
import { ToastType, toastTypes } from "components/Toast";

import StatusToast from "./StatusToast";

const SettingsCooling = () => {
  const [toastType, setToastType] = useState<ToastType | null>(null);

  const { setCoolingMode } = useCoolingMode();

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
      <Cooling onChange={onChangeFanMode} />
    </>
  );
};

export default SettingsCooling;
