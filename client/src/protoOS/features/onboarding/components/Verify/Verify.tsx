import { useNetworkInfo, useSystemInfo } from "@/protoOS/api";
import { FoundMiners, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Verify = () => {
  const { data: networkInfo } = useNetworkInfo();
  const { data: systemInfo } = useSystemInfo({ poll: false });

  const navigate = useNavigate();

  function handleContinue() {
    navigate("/onboarding/authentication");
  }

  return (
    <OnboardingLayout>
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
    </OnboardingLayout>
  );
};

export default Verify;
