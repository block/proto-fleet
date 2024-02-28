import { action } from "@storybook/addon-actions";

import Button, { sizes, variants } from "components/Button";

import Spinner from ".";

export const Default = () => {
  return <Spinner />;
};

export const InButton = () => {
  return (
    <Button
      onClick={() => action("Test Connection")()}
      disabled
      size={sizes.compact}
      text="Test Connection"
      loading
      variant={variants.secondary}
    />
  );
};

export default {
  title: "Spinner",
};
