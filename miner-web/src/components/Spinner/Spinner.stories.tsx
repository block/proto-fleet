import { action } from "@storybook/addon-actions";

import Button, { sizes, variants } from "components/Button";

import SpinnerComponent from ".";

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
  title: "Components/Loaders/Spinner",
  args: {
    inButton: false,
  },
  argTypes: {
    inButton: {
      control: "boolean",
    },
  },
};
