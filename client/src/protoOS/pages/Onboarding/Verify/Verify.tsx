import { useNetworkInfo, useSystemInfo } from "@/protoOS/api";
import { FoundMiners } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Verify = () => {
  const { data: networkInfo } = useNetworkInfo();
  const { data: systemInfo } = useSystemInfo();

  const navigate = useNavigate();

  function handleContinue() {
    navigate("/onboarding/authentication");
  }

  return (
    <FoundMiners
      miners={[
        {
          macAddress: networkInfo?.mac || "Pending",
          controllerSerial: systemInfo?.cb_sn || "Pending",
        },
      ]}
      handleContinueSetup={handleContinue}
    />
  );
};

export default Verify;
