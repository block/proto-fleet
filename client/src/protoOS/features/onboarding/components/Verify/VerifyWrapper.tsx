import Verify from "./Verify";
import { useNetworkInfo } from "@/protoOS/api";
import { useSystemInfo } from "@/protoOS/store";
import { OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const VerifyWrapper = () => {
  // TODO: We should refactor onboarding page to render as a child of App.tsx in the router
  // so that we dont need to make this call here
  const { data: networkInfo } = useNetworkInfo({ poll: false });
  const systemInfo = useSystemInfo();

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
