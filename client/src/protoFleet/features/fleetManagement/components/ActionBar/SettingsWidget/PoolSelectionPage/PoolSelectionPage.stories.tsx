import type { ReactNode } from "react";
import PoolSelectionPageComponent from "./PoolSelectionPage";
import { MockedPoolApis } from "@/protoFleet/stories/MockedPoolApis";

const withMockedPoolApis = (Story: () => ReactNode) => (
  <MockedPoolApis>
    <Story />
  </MockedPoolApis>
);

interface PoolSelectionPageArgs {
  numberOfMiners: number;
}

export const PoolSelectionPage = ({ numberOfMiners }: PoolSelectionPageArgs) => {
  const deviceIdentifiers = Array.from({ length: numberOfMiners }, (_, i) => `device-${i}`);

  return (
    <PoolSelectionPageComponent
      deviceIdentifiers={deviceIdentifiers}
      onAssignPools={async () => {}}
      onDismiss={() => {}}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pool selection page",
  decorators: [withMockedPoolApis],
  args: {
    numberOfMiners: 1,
  },
  argTypes: {
    numberOfMiners: {
      control: { type: "range", min: 1, max: 25, step: 1 },
    },
  },
};
