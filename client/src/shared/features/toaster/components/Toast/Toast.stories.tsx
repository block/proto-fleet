import { action } from "storybook/actions";

import { ToastProps } from "../../types";
import ToastComponent from "./Toast";

export const Toast = ({ status }: ToastProps) => (
  <ToastComponent
    message="This is a toast message"
    onClose={() => {
      action("onClose")();
    }}
    status={status}
  />
);

export default {
  title: "Shared/Toast",
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
