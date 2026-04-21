import { useNetworkInfo } from "@/protoOS/api";
import { Network, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const NetworkPage = () => {
  // TODO: We should refactor onboarding page to render as a child of App.tsx in the router
  // so that we dont need to make this call here
  const { data: networkInfo } = useNetworkInfo({ poll: false });
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
