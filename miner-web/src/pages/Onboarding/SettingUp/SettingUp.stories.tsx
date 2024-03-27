import { useCallback, useMemo } from "react";
import { action } from "@storybook/addon-actions";

import { statuses } from "./constants";
import SettingUpComponent from "./SettingUp";

interface SettingUpProps {
  fanStatus: keyof typeof statuses;
  poolStatus: keyof typeof statuses;
}

export const SettingUp = ({ fanStatus, poolStatus }: SettingUpProps) => {
  const isConfigured = useCallback(
    (status: keyof typeof statuses) =>
      status === statuses.success || status === statuses.error,
    []
  );

  const isSetupDone = useMemo(
    () => isConfigured(poolStatus) && isConfigured(fanStatus),
    [fanStatus, isConfigured, poolStatus]
  );

  return (
    <div className="h-screen flex justify-center items-center">
      <div className="w-[600px]">
        <SettingUpComponent
          fanStatus={fanStatus}
          poolStatus={poolStatus}
          isSetupDone={isSetupDone}
          onClickContinue={action("Continue clicked")}
          onClickRetry={action("Retry clicked")}
        />
      </div>
    </div>
  );
};

export default {
  title: "Pages/Onboarding/Setting Up",
  args: {
    fanStatus: statuses.error,
    poolStatus: statuses.success,
  },
  argTypes: {
    fanStatus: { control: "select", options: Object.keys(statuses) },
    poolStatus: { control: "select", options: Object.keys(statuses) },
  },
};
