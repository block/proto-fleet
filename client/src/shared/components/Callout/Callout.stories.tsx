import { useMemo } from "react";
import { action } from "storybook/actions";

import CalloutComponent, { intents } from ".";
import { BaseIcon } from "@/shared/stories/icons";

interface CalloutArgs {
  hasButton: boolean;
  hasShortSubtitle: boolean;
  hasSubtitle: boolean;
  dismissible: boolean;
}

const CalloutSingleSubtitle = ({
  intent,
  hasButton,
  hasSubtitle,
  header,
  subtitle,
  dismissible,
}: {
  intent: keyof typeof intents;
  hasSubtitle: boolean;
  hasButton: boolean;
  header?: string;
  subtitle?: "long" | "short";
  dismissible: boolean;
}) => {
  const sub = useMemo(() => {
    if (!hasSubtitle) {
      return undefined;
    }
    if (subtitle === "short") {
      return "Subtitle";
    }
    return "Long long long long long long long long long long long long long long long long long long long long long long long subtitle";
  }, [subtitle, hasSubtitle]);

  return (
    <CalloutComponent
      buttonOnClick={() => action("Button clicked")(intent)}
      buttonText={hasButton ? "Button" : undefined}
      intent={intent}
      subtitle={sub}
      title="Title"
      header={header}
      prefixIcon={<BaseIcon />}
      dismissible={dismissible}
      onDismiss={action("Dismiss callout")}
    />
  );
};

export const Callout = ({ hasButton, hasShortSubtitle, hasSubtitle, dismissible }: CalloutArgs) => {
  return (
    <div className="flex flex-col space-y-4">
      <CalloutSingleSubtitle
        intent={intents.default}
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
        dismissible={dismissible}
      />

      <CalloutSingleSubtitle
        intent={intents.information}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
        dismissible={dismissible}
      />

      <CalloutSingleSubtitle
        intent={intents.success}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
        dismissible={dismissible}
      />

      <CalloutSingleSubtitle
        intent={intents.warning}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
        dismissible={dismissible}
      />

      <CalloutSingleSubtitle
        intent={intents.danger}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
        dismissible={dismissible}
      />
    </div>
  );
};

export default {
  title: "Shared/Callout",
  args: {
    hasButton: true,
    hasShortSubtitle: true,
    hasSubtitle: true,
    dismissible: false,
  },
};
