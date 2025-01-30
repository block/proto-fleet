import { action } from "@storybook/addon-actions";

import ToastComponent from "./Toast";
import { ToastProps } from "./types";

export const Toast = ({ status }: ToastProps) => (
  <ToastComponent
    message="This is a toast message"
    onClose={() => { action("onClose")() }}
    status={status}
  />
);

export default {
  title: "Components/Toast",
  args: {
    type: "success",
  },
  argTypes: {
    type: {
      control: "select",
      options: ["success", "error", "loading"],
    },
  },
};
