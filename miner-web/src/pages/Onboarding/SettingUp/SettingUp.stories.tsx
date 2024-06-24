import { useCallback, useMemo } from "react";
import { action } from "@storybook/addon-actions";

import { statuses } from "./constants";
import SettingUpComponent from "./SettingUp";

interface SettingUpProps {
  poolStatus: keyof typeof statuses;
}

export const SettingUp = ({ poolStatus }: SettingUpProps) => {
  const isConfigured = useCallback(
    (status: keyof typeof statuses) =>
      status === statuses.success || status === statuses.error,
    []
  );

  const isSetupDone = useMemo(
    () => isConfigured(poolStatus),
    [isConfigured, poolStatus]
  );

  return (
    <div className="h-screen flex justify-center items-center">
      <div className="w-[600px]">
        <SettingUpComponent
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
    poolStatus: statuses.success,
  },
  argTypes: {
    poolStatus: { control: "select", options: Object.keys(statuses) },
  },
};
