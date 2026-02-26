import { useMemo, useState } from "react";

import { ProtoOSStatusModal } from "@/protoOS/components/StatusModal";
import { useMinerStatusTitle } from "@/protoOS/hooks/status";
import { useHasIssues } from "@/protoOS/store";
import { Alert } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Callout, { intents } from "@/shared/components/Callout";
import { useLocalStorage } from "@/shared/hooks/useLocalStorage";

const dismissedKey = "error-callout-dismissed";

const ErrorCallout = () => {
  const { getItem, setItem } = useLocalStorage();
  const [dismissed, setDismissed] = useState(getItem(dismissedKey));

  // Get status data using individual hooks
  const hasIssues = useHasIssues();
  const { title, subtitle } = useMinerStatusTitle();

  // Local state for modal visibility
  const [isModalOpen, setModalOpen] = useState(false);

  const prefixIcon = useMemo(() => {
    if (hasIssues) {
      return <Alert width={iconSizes.medium} />;
    }

    return undefined;
  }, [hasIssues]);

  return (
    <>
      {hasIssues && !dismissed && (
        <div className="mb-10">
          <Callout
            buttonOnClick={() => setModalOpen(true)}
            buttonText="View details"
            intent={intents.information}
            prefixIcon={prefixIcon}
            title={title}
            subtitle={subtitle}
            dismissible={true}
            onDismiss={() => {
              setItem(dismissedKey, true, 1000 * 60 * 60); // dismissal expires in 1hr
              setDismissed(true);
            }}
          />
        </div>
      )}

      <ProtoOSStatusModal open={isModalOpen} onClose={() => setModalOpen(false)} />
    </>
  );
};

export default ErrorCallout;
