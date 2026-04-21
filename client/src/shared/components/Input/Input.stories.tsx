import { action } from "storybook/actions";

import InputComponent from ".";

interface InputArgs {
  dismiss?: boolean;
  compact?: boolean;
  hideLabelOnFocus?: boolean;
}

export const Input = ({ dismiss, compact, hideLabelOnFocus }: InputArgs) => {
  return (
    <div className="space-y-4">
      <InputComponent
        id="username"
        label="Username"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange username")(value)}
      />
      <InputComponent
        id="password"
        label="Password"
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange password")(value)}
        type="password"
      />
      <InputComponent
        id="disabled"
        label="Disabled"
        compact={compact}
        onChange={(value) => action("onChange disabled")(value)}
        type="disabled"
        disabled
      />
      <InputComponent
        id="error"
        label="Error field"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        error="This is an error message"
        onChange={(value) => action("onChange error")(value)}
      />
      <InputComponent
        id="error-without-message"
        label="Error without message"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange error without message")(value)}
        error
      />
      <InputComponent
        id="poolUrl"
        label="Pool URL"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        maxLength={2083}
        tooltip={{
          header: "Mining Pool URL",
          body: "Enter the mining pool URL you want this miner to connect with. A mining pool URL allows this miner to communicate with the pool's server.",
        }}
        onChange={(value) => action("onChange pool url")(value)}
      />
      <InputComponent
        id="power-target"
        label="Power target"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange power target")(value)}
        type="number"
        units="kW"
      />
    </div>
  );
};

export default {
  title: "Shared/Input",
  args: {
    dismiss: false,
    compact: false,
    hideLabelOnFocus: false,
  },
};
