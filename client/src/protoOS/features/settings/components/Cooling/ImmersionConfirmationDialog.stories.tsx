import { action } from "storybook/actions";
import ImmersionConfirmationDialogComponent from "./ImmersionConfirmationDialog";

interface ImmersionConfirmationDialogArgs {
  show: boolean;
  isLoading: boolean;
}

export const ImmersionConfirmationDialog = ({
  show,
  isLoading,
}: ImmersionConfirmationDialogArgs) => {
  return (
    <ImmersionConfirmationDialogComponent
      show={show}
      onDismiss={action("onDismiss")}
      onConfirm={action("onConfirm")}
      isLoading={isLoading}
    />
  );
};

export default {
  title: "ProtoOS/Settings/Immersion Confirmation Dialog",
  args: {
    show: true,
    isLoading: false,
  },
  argTypes: {
    show: {
      control: { type: "boolean" },
      description: "Controls dialog visibility",
    },
    isLoading: {
      control: { type: "boolean" },
      description: "Shows loading state with disabled buttons",
    },
  },
};
