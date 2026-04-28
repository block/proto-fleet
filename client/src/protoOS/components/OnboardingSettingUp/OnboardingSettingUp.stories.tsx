import { useCallback, useMemo } from "react";
import { action } from "storybook/actions";

import OnboardingSettingUp from "@/shared/components/OnboardingSettingUp/OnboardingSettingUp";
import { statuses } from "@/shared/constants/statuses";

interface OnboardingSettingUpProps {
  poolStatus: keyof typeof statuses;
}

export const SettingUp = ({ poolStatus }: OnboardingSettingUpProps) => {
  const isConfigured = useCallback((status: keyof typeof statuses) => status === statuses.success, []);

  const isSetupDone = useMemo(() => isConfigured(poolStatus), [isConfigured, poolStatus]);

  return (
    <div className="flex h-screen items-center justify-center">
      <div className="w-[600px]">
        <OnboardingSettingUp
          poolStatus={poolStatus}
          isSetupDone={isSetupDone}
          onClickContinue={action("Continue clicked")}
          onClickReconfigure={action("Reconfigure clicked")}
          onClickRetry={action("Retry clicked")}
        />
      </div>
    </div>
  );
};

export default {
  title: "Proto OS/Onboarding/Setting Up",
  args: {
    poolStatus: statuses.success,
  },
  argTypes: {
    poolStatus: { control: "select", options: Object.keys(statuses) },
  },
};
