import { useNetworkInfo, useSystemInfo } from "@/protoOS/api";
import { FoundMiners } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Verify = () => {
  const { data: networkInfo } = useNetworkInfo();
  const { data: systemInfo } = useSystemInfo({ poll: false });

  const navigate = useNavigate();

  function handleContinue() {
    navigate("/onboarding/authentication");
  }

  return (
    <FoundMiners
      miners={[
        {
          macAddress: networkInfo?.mac || "Pending",
          serialNumber: systemInfo?.cb_sn || "Pending",
        },
      ]}
      handleContinueSetup={handleContinue}
      handleRestartSearch={() => {}}
    />
  );
};

export default Verify;
