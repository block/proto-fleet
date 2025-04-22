import { WelcomeScreen } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Welcome = () => {
  const navigate = useNavigate();

  function handleSearch() {
    navigate("/onboarding/verify");
  }

  function handleRetry() {}

  return (
    <WelcomeScreen
      searching={false}
      handleSearch={handleSearch}
      noMinersFound={false}
      handleRetry={handleRetry}
    />
  );
};

export default Welcome;
