import { action } from "@storybook/addon-actions";

import Input from ".";

export const Single = () => {
  return (
    <>
      <Input
        id="poolUrl"
        label="Pool URL"
        onChange={(value) => action("onChange pool url")(value)}
        maxLength={2083}
      />
    </>
  );
};

export const Multiple = () => {
  return (
    <>
      <Input
        id="poolUrl"
        label="Pool URL"
        onChange={(value) => action("onChange pool url")(value)}
        maxLength={2083}
      />
      <Input
        id="username"
        label="Username"
        onChange={(value) => action("onChange username")(value)}
      />
      <Input
        id="password"
        label="Password"
        onChange={(value) => action("onChange password")(value)}
        type="password"
      />
    </>
  );
};

export default {
  title: "Input",
};
