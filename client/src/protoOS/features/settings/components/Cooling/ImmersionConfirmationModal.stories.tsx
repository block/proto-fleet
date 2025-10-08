import { action } from "storybook/actions";
import ImmersionConfirmationModalComponent from "./ImmersionConfirmationModal";

interface ImmersionConfirmationModalArgs {
  isLoading: boolean;
}

export const ImmersionConfirmationModal = ({
  isLoading,
}: ImmersionConfirmationModalArgs) => {
  return (
    <ImmersionConfirmationModalComponent
      onDismiss={action("onDismiss")}
      onConfirm={action("onConfirm")}
      isLoading={isLoading}
    />
  );
};

export default {
  title: "Shared/Immersion Confirmation Modal",
  args: {
    isLoading: false,
  },
  argTypes: {
    isLoading: {
      control: { type: "boolean" },
      description: "Shows loading state with disabled buttons",
    },
  },
};
