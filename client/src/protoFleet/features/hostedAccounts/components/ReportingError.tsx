import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";

export const ReportingError = ({ error }: { error: string }) => (
  <div role="alert">
    <Callout intent="danger" prefixIcon={<Alert />} title={error} />
  </div>
);
