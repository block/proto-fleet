import { ReportingError } from "./ReportingError";
import Button from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

export const AccountState = ({
  loading,
  error,
  retry,
}: {
  loading?: boolean;
  error?: string | null;
  retry: () => void;
}) =>
  loading ? (
    <div className="flex justify-center p-10">
      <ProgressCircular indeterminate />
    </div>
  ) : error ? (
    <div className="flex flex-col gap-4">
      <ReportingError error={error} />
      <Button variant="secondary" text="Retry" onClick={retry} />
    </div>
  ) : null;
