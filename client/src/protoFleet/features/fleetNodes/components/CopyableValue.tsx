import { useCallback } from "react";

import { Copy } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

interface CopyableValueProps {
  value: string;
  ariaLabel: string;
}

const CopyableValue = ({ value, ariaLabel }: CopyableValueProps) => {
  const handleCopy = useCallback(() => {
    copyToClipboard(value)
      .then(() => pushToast({ message: "Copied to clipboard", status: STATUSES.success }))
      .catch(() => pushToast({ message: "Failed to copy", status: STATUSES.error }));
  }, [value]);

  return (
    <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-4">
      <div className="font-mono text-300 break-all text-text-primary">{value}</div>
      <Button variant="ghost" onClick={handleCopy} ariaLabel={ariaLabel} prefixIcon={<Copy />} />
    </div>
  );
};

export default CopyableValue;
