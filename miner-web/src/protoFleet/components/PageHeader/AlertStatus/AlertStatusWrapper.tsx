import AlertStatus from "./AlertStatus";

const AlertStatusWrapper = () => {
  // TODO load alerts from API
  const alertsCount = 324;
  const loading = false;

  return <AlertStatus alertsCount={alertsCount} loading={loading} />;
};

export default AlertStatusWrapper;
