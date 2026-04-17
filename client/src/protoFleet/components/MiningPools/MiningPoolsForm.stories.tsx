import type { ReactNode } from "react";
import { action } from "storybook/actions";
import MiningPoolsFormComponent from "@/protoFleet/components/MiningPools/MiningPoolsForm";
import { MockedPoolApis } from "@/protoFleet/stories/MockedPoolApis";

const withMockedPoolApis = (Story: () => ReactNode) => (
  <MockedPoolApis>
    <Story />
  </MockedPoolApis>
);

interface MiningPoolsFormArgs {
  buttonLabel: string;
}

export const MiningPoolsForm = ({ buttonLabel }: MiningPoolsFormArgs) => {
  return (
    <MiningPoolsFormComponent
      buttonLabel={buttonLabel}
      onSaveRequested={action("Save requested")}
      onSaveDone={() => {}}
    />
  );
};

export default {
  title: "Proto Fleet/MiningPoolsForm",
  decorators: [withMockedPoolApis],
  args: {
    buttonLabel: "Continue",
  },
};
