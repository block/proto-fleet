import { SetupHeader as SetupHeaderComponent } from ".";
import { Step } from "@/shared/components/Setup/setupHeader.types";

type SetupHeaderProps = {
  activeStep: Step;
};

export const SetupHeader = ({ activeStep }: SetupHeaderProps) => {
  return (
    <div>
      <SetupHeaderComponent activeStep={activeStep} />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Setup Header",
  args: {
    activeStep: "network",
  },
  argTypes: {
    activeStep: {
      type: "select",
      options: ["network", "authentication", "miningPool"],
    },
  },
};
