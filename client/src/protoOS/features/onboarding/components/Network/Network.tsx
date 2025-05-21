import { useNetworkInfo } from "@/protoOS/api";
import { Network, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const NetworkPage = () => {
  const { data: networkInfo } = useNetworkInfo();
  const navigate = useNavigate();

  return (
    <OnboardingLayout>
      <Network
        submit={() => {
          // TODO: Send network info to the API
          navigate("/onboarding/authentication");
        }}
        subnet={networkInfo?.ip || "Pending"}
        gateway={networkInfo?.gateway || "Pending"}
      />
    </OnboardingLayout>
  );
};

export default NetworkPage;
