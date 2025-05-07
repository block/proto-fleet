import { action } from "@storybook/addon-actions";

import InputComponent from ".";

export const Input = () => {
  return (
    <div className="space-y-4">
      <InputComponent
        id="poolUrl"
        label="Pool URL"
        onChange={(value) => action("onChange pool url")(value)}
        maxLength={2083}
      />
      <InputComponent
        id="username"
        label="Username"
        onChange={(value) => action("onChange username")(value)}
      />
      <InputComponent
        id="password"
        label="Password"
        onChange={(value) => action("onChange password")(value)}
        type="password"
      />
      <InputComponent
        id="disabled"
        label="Disabled"
        onChange={(value) => action("onChange disabled")(value)}
        type="disabled"
        disabled
      />
      <InputComponent
        id="error"
        label="Error field"
        onChange={(value) => action("onChange error")(value)}
        type="error"
        error="This is an error message"
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Input",
};
