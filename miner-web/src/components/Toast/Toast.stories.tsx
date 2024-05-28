import { action } from "@storybook/addon-actions";

import ToastComponent, { ToastType } from ".";

interface ToastProps {
  type: ToastType;
}

export const Toast = ({ type }: ToastProps) => (
  <ToastComponent
    message="This is a toast message"
    onClose={() => { action("onClose")() }}
    type={type}
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
