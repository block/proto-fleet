import OfflineMinersComponent from ".";

interface OfflineMinersArgs {
  activeMiners: number;
  inactiveMiners: number;
  offlineMiners: number;
}

export const OfflineMiners = ({
  activeMiners,
  inactiveMiners,
  offlineMiners,
}: OfflineMinersArgs) => {
  return (
    <OfflineMinersComponent
      activeMiners={activeMiners}
      inactiveMiners={inactiveMiners}
      offlineMiners={offlineMiners}
    />
  );
};

export default {
  title: "Components (ProtoFleet)/Offline Miners",
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
