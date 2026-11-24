import DurationSelectorComponent, { fleetDurations } from ".";

export const DurationSelector = () => {
  return <DurationSelectorComponent />;
};

export const Fleet = () => <DurationSelectorComponent durations={fleetDurations} />;

export default {
  title: "Shared/Segmented Control/Duration Selector",
};
