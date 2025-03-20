import { Alert, Checkmark } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Chip from "@/shared/components/Chip";

interface AlertStatusProps {
  alertsCount?: number;
  loading?: boolean;
}

const AlertStatus = ({ alertsCount, loading = false }: AlertStatusProps) => {
  const text = () => {
    if (loading) return "Alerts";
    if (alertsCount === 0) return "No alerts";

    return alertsCount + " Alerts";
  };

  // TODO some alerts probably have higher severity, then icon should be red
  const icon = () => {
    const color =
      (alertsCount ?? 0) > 0 ? "text-text-warning" : "text-text-success";

    if (alertsCount === 0)
      return <Checkmark className={color} width={iconSizes.small} />;
    return <Alert className={color} width={iconSizes.small} />;
  };

  return (
    <Chip loading={loading} prefixIcon={icon()}>
      {text()}
    </Chip>
  );
};

export default AlertStatus;
