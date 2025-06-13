import { action } from "@storybook/addon-actions";
import MiningPoolsFormComponent from "@/protoFleet/components/MiningPools/MiningPoolsForm";
import { OnboardingProvider } from "@/protoFleet/features/onboarding/contexts/OnboardingContext";

interface MiningPoolsFormArgs {
  buttonLabel: string;
}

export const MiningPoolsForm = ({ buttonLabel }: MiningPoolsFormArgs) => {
  return (
    <OnboardingProvider>
      <MiningPoolsFormComponent
        buttonLabel={buttonLabel}
        onSaveRequested={action("Save requested")}
        onSaveDone={() => {}}
      />
    </OnboardingProvider>
  );
};

export default {
  title: "Components (protoFleet)/MiningPoolsForm",
  args: {
    buttonLabel: "Continue",
  },
};
