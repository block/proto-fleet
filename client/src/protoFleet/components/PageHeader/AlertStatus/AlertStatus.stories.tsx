import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import AlertStatusComponent from "./AlertStatus";
import { alerts } from "@/protoFleet/features/fleetManagement/components/AlertsModal/stories/mocks";

interface AlertStatusArgs {
  loading: boolean;
  numberOfAlerts: number;
}

export const AlertStatus = ({ loading, numberOfAlerts }: AlertStatusArgs) => {
  return (
    <AlertStatusComponent
      loading={loading}
      alerts={alerts.slice(0, numberOfAlerts)}
    />
  );
};

export default {
  title: "Components (protoFleet)/Page Header/Alert Status",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  args: {
    loading: false,
    numberOfAlerts: 1,
  },
  argTypes: {
    numberOfAlerts: { control: { type: "range", min: 0, max: 5, step: 1 } },
  },
};
