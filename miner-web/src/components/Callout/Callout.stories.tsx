import { action } from "@storybook/addon-actions";

import { BaseIcon } from "common/stories/icons";

import CalloutComponent, { intents } from ".";

const CalloutSingleSubtitle = ({
  intent,
  subtitle = "short",
}: {
  intent: keyof typeof intents;
  subtitle?: "long" | "short";
}) => (
  <CalloutComponent
    buttonOnClick={() => action("Button clicked")(intent)}
    buttonText="Button"
    intent={intent}
    subtitle={
      subtitle === "short"
        ? "Subtitle"
        : "Long long long long long long long long long long long long long long long long long long long long long long long subtitle"
    }
    prefixIcon={<BaseIcon />}
  />
);

export const Callout = () => {
  return (
    <div className="flex flex-col space-y-4">
      <CalloutSingleSubtitle intent={intents.default} />
      <CalloutSingleSubtitle intent={intents.default} subtitle="long" />

      <CalloutSingleSubtitle intent={intents.information} />
      <CalloutSingleSubtitle intent={intents.information} subtitle="long" />

      <CalloutSingleSubtitle intent={intents.success} />
      <CalloutSingleSubtitle intent={intents.success} subtitle="long" />

      <CalloutSingleSubtitle intent={intents.warning} />
      <CalloutSingleSubtitle intent={intents.warning} subtitle="long" />

      <CalloutSingleSubtitle intent={intents.danger} />
      <CalloutSingleSubtitle intent={intents.danger} subtitle="long" />
    </div>
  );
};

export default {
  title: "Components/Callout",
};
