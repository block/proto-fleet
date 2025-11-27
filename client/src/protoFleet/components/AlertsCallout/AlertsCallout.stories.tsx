import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import AlertsCalloutComponent from ".";
import { alerts } from "@/protoFleet/features/fleetManagement/components/AlertsModal/stories/mocks";

interface AlertsCalloutArgs {
  numberOfAlerts: number;
  numberOfMinersInFleet: number;
}

export const AlertsCallout = ({ numberOfAlerts, numberOfMinersInFleet }: AlertsCalloutArgs) => {
  return (
    <AlertsCalloutComponent alerts={alerts.slice(0, numberOfAlerts)} numberOfMinersInFleet={numberOfMinersInFleet} />
  );
};

export default {
  title: "Proto Fleet/Alerts Callout",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  args: {
    numberOfAlerts: 1,
    numberOfMinersInFleet: 100,
  },
  argTypes: {
    numberOfAlerts: { control: { type: "range", min: 0, max: 5, step: 1 } },
    numberOfMinersInFleet: {
      control: { type: "range", min: 10, max: 1000, step: 1 },
    },
  },
};
