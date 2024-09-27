import { useCallback, useMemo, useState } from "react";

import { ErrorListResponse } from "apiTypes";

import Callout, { intents } from "components/Callout";
import MinerStatusModal from "components/MinerStatusModal/MinerStatusModal";
import {
  getErrorTitle,
  isError,
  isWarning,
} from "components/MinerStatusModal/utility";

import { Alert, Stop } from "icons";
import { iconSizes } from "icons/constants";

interface ErrorCalloutProps {
  errors: ErrorListResponse;
}

const ErrorCallout = ({ errors }: ErrorCalloutProps) => {
  const [showModal, setShowModal] = useState(false);

  const isPoolError = useCallback(
    (error_code?: string) => /pool/i.test(error_code || ""),
    []
  );

  const hasErrors = useMemo(
    () =>
      errors.some(
        // pool connection errors are tracked in the mining pool widget
        (error) => isError(error.error_level) && !isPoolError(error.error_code)
      ),
    [errors, isPoolError]
  );

  const hasWarnings = useMemo(
    () =>
      errors.some(
        (error) =>
          // pool connection errors are tracked in the mining pool widget
          isWarning(error.error_level) && !isPoolError(error.error_code)
      ),
    [errors, isPoolError]
  );

  const prefixIcon = useMemo(() => {
    if (hasErrors) {
      return <Stop className="text-text-critical" width={iconSizes.medium} />;
    }
    if (hasWarnings) {
      return <Alert className="text-text-warning" width={iconSizes.medium} />;
    }
    return undefined;
  }, [hasErrors, hasWarnings]);

  const title = useMemo(() => getErrorTitle(errors), [errors]);

  return (
    <div className="mb-10">
      {(hasErrors || hasWarnings) && (
        <Callout
          buttonOnClick={() => setShowModal(true)}
          buttonText="View details"
          intent={intents.information}
          prefixIcon={prefixIcon}
          title={title}
        />
      )}
      {showModal && (
        <MinerStatusModal
          errors={errors}
          onDismiss={() => setShowModal(false)}
        />
      )}
    </div>
  );
};

export default ErrorCallout;
