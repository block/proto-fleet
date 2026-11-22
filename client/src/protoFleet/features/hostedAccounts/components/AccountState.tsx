import { Alert } from "@/shared/assets/icons";
import Callout from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";

export const AccountState = ({
  loading,
  error,
  retry,
  title = "Could not load account data",
}: {
  loading?: boolean;
  error?: string | null;
  retry: () => void;
  title?: string;
}) =>
  loading ? (
    <div className="flex justify-center p-10">
      <ProgressCircular indeterminate />
    </div>
  ) : error ? (
    <div role="alert">
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title={title}
        subtitle={error}
        buttonText="Retry"
        buttonOnClick={retry}
      />
    </div>
  ) : null;
