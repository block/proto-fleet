import { useMemo, useState } from "react";

import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Callout, { intents } from "@/shared/components/Callout";
import MinerStatusModal from "@/shared/components/MinerStatusModal";
import { type MinerStatus } from "@/shared/components/MinerStatusModal/types";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const dismissedKey = "error-callout-dismissed";

const ErrorCallout = ({ status }: { status: MinerStatus }) => {
  const [showModal, setShowModal] = useState(false);
  const { getItem, setItem } = useLocalStorage();

  const [dismissed, setDismissed] = useState(getItem(dismissedKey));

  const prefixIcon = useMemo(() => {
    if (status.hasIssues) {
      return <Alert className="text-text-critical" width={iconSizes.medium} />;
    }

    return undefined;
  }, [status.hasIssues]);

  const { isProtoRig } = useSystemContext();

  return (
    <>
      {status.hasIssues && !dismissed && (
        <div className="mb-10">
          <Callout
            buttonOnClick={() => setShowModal(true)}
            buttonText="View details"
            intent={intents.information}
            prefixIcon={prefixIcon}
            title={status.title}
            subtitle={status.subtitle}
            dismissible={true}
            onDismiss={() => {
              setItem(dismissedKey, true, 1000 * 60 * 60); // dismissal expires in 1hr
              setDismissed(true);
            }}
          />
          {showModal && (
            <MinerStatusModal
              status={status}
              onDismiss={() => setShowModal(false)}
              isProtoRig={isProtoRig}
            />
          )}
        </div>
      )}
    </>
  );
};

export default ErrorCallout;
