import { action } from "storybook/actions";

import LoginModalComponent from "./LoginModal";

export const Default = () => {
  return <LoginModalComponent onSuccess={action("succeeded login")} onDismiss={action("dismiss modal")} />;
};

export default {
  title: "ProtoOS/Login Modal",
  component: LoginModalComponent,
};
