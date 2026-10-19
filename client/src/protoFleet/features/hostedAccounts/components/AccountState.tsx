import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
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
    <div className="flex flex-col items-center gap-4 p-6" role="alert">
      <SettingsEmptyState title="Unable to load account data" description={error} />
      <Button variant="secondary" text="Retry" onClick={retry} />
    </div>
  ) : null;
