import Verify from "./Verify";
import { useNetworkInfo } from "@/protoOS/api";
import { useSystemContext } from "@/protoOS/contexts/SystemContext";
import { OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const VerifyWrapper = () => {
  const { data: networkInfo } = useNetworkInfo();
  const { data: systemInfo } = useSystemContext();

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
