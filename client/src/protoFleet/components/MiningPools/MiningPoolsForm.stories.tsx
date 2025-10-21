import { action } from "storybook/actions";
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
  title: "Proto Fleet/MiningPoolsForm",
  args: {
    buttonLabel: "Continue",
  },
};
