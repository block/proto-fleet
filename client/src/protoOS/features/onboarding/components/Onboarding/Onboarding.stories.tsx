import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import { action } from "storybook/actions";

import OnboardingComponent from "./Onboarding";

interface OnboardingProps {
  settingUpMiner: boolean;
}

export const Onboarding = ({ settingUpMiner }: OnboardingProps) => {
  return (
    <OnboardingComponent
      settingUpMiner={settingUpMiner}
      onChangeSettingUpMiner={action("onChangeSettingUpMiner")}
    />
  );
};

export default {
  title: "ProtoOS/Onboarding/Mining Pools",
  args: {
    settingUpMiner: false,
  },
  argTypes: {
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
