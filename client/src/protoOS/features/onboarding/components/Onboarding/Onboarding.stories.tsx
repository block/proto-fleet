import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "storybook/actions";

import OnboardingComponent from "./Onboarding";

interface OnboardingProps {
  pendingNetworkInfo: boolean;
  settingUpMiner: boolean;
}

export const Onboarding = ({
  pendingNetworkInfo,
  settingUpMiner,
}: OnboardingProps) => {
  return (
    <OnboardingComponent
      networkInfo={{ mac: "42:08:59:58:84:c6" }}
      pendingNetworkInfo={pendingNetworkInfo}
      settingUpMiner={settingUpMiner}
      onChangeSettingUpMiner={action("onChangeSettingUpMiner")}
    />
  );
};

export default {
  title: "ProtoOS/Onboarding/Mining Pools",
  args: {
    pendingNetworkInfo: false,
    settingUpMiner: false,
  },
  argTypes: {
    pendingNetworkInfo: {
      control: "boolean",
    },
    settingUpMiner: {
      control: "boolean",
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
