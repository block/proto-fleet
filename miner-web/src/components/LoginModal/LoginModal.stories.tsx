import { action } from "@storybook/addon-actions";

import LoginModal from "./LoginModal";

export const Login = () => {
  return <LoginModal onContinue={action("continue")} onDismiss={action("dismiss modal")} />;
};

export default {
  title: "Pages/Auth/Login",
};
