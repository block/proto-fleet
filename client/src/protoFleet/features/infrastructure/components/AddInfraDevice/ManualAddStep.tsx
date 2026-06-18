import { useCallback, useEffect, useState } from "react";

import Input from "@/shared/components/Input";

interface ManualAddStepProps {
  onSuccess: () => void;
  onValidChange: (valid: boolean, pairHandler: () => void) => void;
}

const ManualAddStep = ({ onSuccess, onValidChange }: ManualAddStepProps) => {
  const [ipAddress, setIpAddress] = useState("");

  const isValid = /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(ipAddress.trim());

  const handlePair = useCallback(() => {
    if (!isValid) return;
    onSuccess();
  }, [isValid, onSuccess]);

  useEffect(() => {
    onValidChange(isValid, handlePair);
  }, [isValid, handlePair, onValidChange]);

  return (
    <div className="flex flex-col gap-4 py-2">
      <span className="text-300 text-text-primary-70">Enter the IP address of the device to pair</span>
      <Input id="manual-ip" label="IP address" onChange={(v) => setIpAddress(v)} />
    </div>
  );
};

export default ManualAddStep;
