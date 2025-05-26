import { action } from "@storybook/addon-actions";
import MiningPoolsFormComponent from "@/protoFleet/components/MiningPools/MiningPoolsForm";

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
  title: "Components (protoFleet)/MiningPoolsForm",
  args: {
    buttonLabel: "Continue",
  },
};
