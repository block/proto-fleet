import { action } from "@storybook/addon-actions";

import SpinnerComponent from ".";
import Button, { sizes, variants } from "@/shared/components/Button";

interface SpinnerProps {
  inButton?: boolean;
}

export const Spinner = ({ inButton }: SpinnerProps) => {
  if (inButton) {
    return (
      <Button
        onClick={action("Test Connection")}
        disabled
        size={sizes.compact}
        text="Test Connection"
        loading
        variant={variants.secondary}
      />
    );
  }
  return <SpinnerComponent />;
};

export default {
  title: "Components (Shared)/Loaders/Spinner",
  args: {
    inButton: false,
  },
  argTypes: {
    inButton: {
      control: "boolean",
    },
  },
};
