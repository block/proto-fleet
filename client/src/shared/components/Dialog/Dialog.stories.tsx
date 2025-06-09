import { action } from "@storybook/addon-actions";
import DialogComponent from ".";
import { variants } from "@/shared/components/Button";

export const Dialog = () => {
  return (
    <DialogComponent
      title="Title"
      subtitle="Description"
      show
      buttons={[
        {
          text: "Secondary",
          variant: variants.secondary,
          onClick: action("Secondary clicked"),
        },
        {
          text: "Primary",
          variant: variants.primary,
          onClick: action("Primary clicked"),
        },
      ]}
    />
  );
};

export const LoadingDialog = () => {
  return (
    <DialogComponent
      title="Connecting to your mining pool"
      subtitle="This may take a few seconds"
      loading
      show
    />
  );
};

export default {
  title: "Components (Shared)/Dialog",
};
