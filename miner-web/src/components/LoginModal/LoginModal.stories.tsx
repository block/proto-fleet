import { action } from "@storybook/addon-actions";

import LoginModalComponent from "./LoginModal";

export const LoginModal = () => {
  return (
    <LoginModalComponent
      onSuccess={action("succeeded login")}
      onDismiss={action("dismiss modal")}
    />
  );
};

export default {
  title: "Components/Login Modal",
};
