import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "@storybook/addon-actions";

import OnboardingComponent from "./Onboarding";

interface OnboardingProps {
  pendingNetworkInfo: boolean;
  pendingSystemInfo: boolean;
  settingUpMiner: boolean;
}

export const Onboarding = ({
  pendingNetworkInfo,
  pendingSystemInfo,
  settingUpMiner,
}: OnboardingProps) => {
  return (
    <OnboardingComponent
      networkInfo={{ mac: "42:08:59:58:84:c6" }}
      pendingNetworkInfo={pendingNetworkInfo}
      systemInfo={{ os: { version: "0.2.45" } }}
      pendingSystemInfo={pendingSystemInfo}
      settingUpMiner={settingUpMiner}
      onChangeSettingUpMiner={action("onChangeSettingUpMiner")}
    />
  );
};

export default {
  title: "Pages/Onboarding",
  args: {
    pendingNetworkInfo: false,
    pendingSystemInfo: false,
    settingUpMiner: false,
  },
  argTypes: {
    pendingNetworkInfo: {
      control: "boolean",
    },
    pendingSystemInfo: {
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
