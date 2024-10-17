import { action } from "@storybook/addon-actions";

import LoginModalComponent from "./LoginModal";

export const LoginModal = () => {
  return (
    <LoginModalComponent
      onContinue={action("continue")}
      onDismiss={action("dismiss modal")}
    />
  );
};

export default {
  title: "Components/Login Modal",
};
