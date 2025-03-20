import AlertStatusComponent from "./AlertStatus";

interface AlertStatusArgs {
  loading: boolean;
  alertsCount: number;
}

export const AlertStatus = ({ loading, alertsCount }: AlertStatusArgs) => {
  return <AlertStatusComponent loading={loading} alertsCount={alertsCount} />;
};

export default {
  title: "Components (protoFleet)/Page Header/Alert Status",
  args: {
    loading: false,
    alertsCount: 0,
  },
  argTypes: {
    alertsCount: { control: { type: "number", min: 0, max: 1000, step: 1 } },
  },
};
