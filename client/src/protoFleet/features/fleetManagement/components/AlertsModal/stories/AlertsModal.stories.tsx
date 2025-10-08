import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import AlertsModalComponent from "../AlertsModal";
import { alerts } from "@/protoFleet/features/fleetManagement/components/AlertsModal/stories/mocks";

interface AlertsModalArgs {
  numberOfAlerts: number;
}

export const AlertsModal = ({ numberOfAlerts }: AlertsModalArgs) => {
  return (
    <AlertsModalComponent
      show
      alerts={[...alerts, ...alerts, ...alerts].slice(0, numberOfAlerts)}
      onDismiss={() => {}}
    />
  );
};

export default {
  title: "Proto Fleet/Alerts Modal",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  args: {
    numberOfAlerts: 5,
  },
  argTypes: {
    numberOfAlerts: { control: { type: "range", min: 0, max: 15, step: 1 } },
  },
};
