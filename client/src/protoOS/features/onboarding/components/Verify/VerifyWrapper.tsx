import Verify from "./Verify";
import { useNetworkInfo, useSystemInfo } from "@/protoOS/api";
import { OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const VerifyWrapper = () => {
  const { data: networkInfo } = useNetworkInfo();
  const { data: systemInfo } = useSystemInfo({ poll: false });

  const navigate = useNavigate();

  function handleContinue() {
    navigate("/onboarding/authentication");
  }

  return (
    <OnboardingLayout>
      <Verify
        miner={{
          macAddress: networkInfo?.mac || "Pending",
          serialNumber: systemInfo?.cb_sn || "Pending",
        }}
        handleContinueSetup={handleContinue}
      />
    </OnboardingLayout>
  );
};

export default VerifyWrapper;
