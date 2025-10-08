import OfflineMinersComponent from ".";

interface OfflineMinersArgs {
  fleetSize: number;
  activeMiners: number;
  inactiveMiners: number;
  offlineMiners: number;
}

export const OfflineMiners = ({
  fleetSize,
  activeMiners,
  inactiveMiners,
  offlineMiners,
}: OfflineMinersArgs) => {
  return (
    <OfflineMinersComponent
      fleetSize={fleetSize}
      activeMiners={activeMiners}
      inactiveMiners={inactiveMiners}
      offlineMiners={offlineMiners}
    />
  );
};

export default {
  title: "Proto Fleet/Offline Miners",
  args: {
    activeMiners: 210,
    inactiveMiners: 12,
    offlineMiners: 3,
  },
  argTypes: {
    activeMiners: {
      control: { type: "range", min: 0, max: 1000, step: 1 },
    },
    inactiveMiners: {
      control: { type: "range", min: 0, max: 1000, step: 1 },
    },
    offlineMiners: {
      control: { type: "range", min: 0, max: 1000, step: 1 },
    },
  },
};
