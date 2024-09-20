import { useMemo } from "react";
import { action } from "@storybook/addon-actions";

import { BaseIcon } from "common/stories/icons";

import CalloutComponent, { intents } from ".";

const CalloutSingleSubtitle = ({
  intent,
  hasButton,
  hasSubtitle,
  header,
  subtitle,
}: {
  intent: keyof typeof intents;
  hasSubtitle: boolean;
  hasButton: boolean;
  header?: string;
  subtitle?: "long" | "short";
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
    />
  );
};

export const Callout = ({
  hasButton,
  hasShortSubtitle,
  hasSubtitle,
}: {
  hasButton: boolean;
  hasShortSubtitle: boolean;
  hasSubtitle: boolean;
}) => {
  return (
    <div className="flex flex-col space-y-4">
      <CalloutSingleSubtitle
        intent={intents.default}
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
      />

      <CalloutSingleSubtitle
        intent={intents.information}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
      />

      <CalloutSingleSubtitle
        intent={intents.success}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
      />

      <CalloutSingleSubtitle
        intent={intents.warning}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
      />

      <CalloutSingleSubtitle
        intent={intents.danger}
        header="Title"
        subtitle={hasShortSubtitle ? "short" : "long"}
        hasButton={hasButton}
        hasSubtitle={hasSubtitle}
      />
    </div>
  );
};

export default {
  title: "Components/Callout",
  args: {
    hasButton: true,
    hasShortSubtitle: true,
    hasSubtitle: true,
  },
  argTypes: {
    hasButton: {
      control: "boolean",
    },
    hasShortSubtitle: {
      control: "boolean",
    },
    hasSubtitle: {
      control: "boolean",
    },
  },
};
