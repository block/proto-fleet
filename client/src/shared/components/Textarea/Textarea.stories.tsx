import { action } from "storybook/actions";

import TextareaComponent from ".";

interface TextareaArgs {
  dismiss?: boolean;
  compact?: boolean;
  hideLabelOnFocus?: boolean;
}

export const Textarea = ({ dismiss, compact, hideLabelOnFocus }: TextareaArgs) => {
  return (
    <div className="space-y-4">
      <TextareaComponent
        id="ip-address"
        label="IP Addresses"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange ip addresses")(value)}
      />
      <TextareaComponent
        id="mining-pool-urls"
        label="Mining Pool URLs"
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange mining pool urls")(value)}
        rows={3}
      />
      <TextareaComponent
        id="disabled"
        label="Disabled"
        compact={compact}
        onChange={(value) => action("onChange disabled")(value)}
        disabled
        rows={4}
      />
      <TextareaComponent
        id="error"
        label="Error field"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        error="This is an error message"
        onChange={(value) => action("onChange error")(value)}
        rows={3}
      />
      <TextareaComponent
        id="error-without-message"
        label="Error without message"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        onChange={(value) => action("onChange error without message")(value)}
        error
        rows={3}
      />
      <TextareaComponent
        id="with-tooltip"
        label="With Tooltip"
        dismiss={dismiss}
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        tooltip={{
          header: "Textarea Tooltip",
          body: "This is a helpful tooltip for the textarea.",
        }}
        onChange={(value) => action("onChange with tooltip")(value)}
        rows={3}
      />
      <TextareaComponent
        id="with-keyboard-shortcuts"
        label="With Keyboard Shortcuts"
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        keyboardShortcuts={["Ctrl+Enter"]}
        onChange={(value) => action("onChange with keyboard shortcuts")(value)}
        rows={3}
      />
      <TextareaComponent
        id="max-length"
        label="Max Length 100"
        compact={compact}
        hideLabelOnFocus={compact || hideLabelOnFocus}
        maxLength={100}
        onChange={(value) => action("onChange max length")(value)}
        rows={3}
      />
    </div>
  );
};

export default {
  title: "Shared/Textarea",
  args: {
    dismiss: false,
    compact: false,
    hideLabelOnFocus: false,
  },
};
