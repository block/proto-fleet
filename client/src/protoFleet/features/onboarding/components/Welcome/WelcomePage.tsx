import { WelcomeScreen } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const WelcomePage = () => {
  const navigate = useNavigate();

  function handleSearch() {
    navigate("/onboarding/auth");
  }

  return (
    <WelcomeScreen
      searching={false}
      handleSearch={handleSearch}
      noMinersFound={false}
      handleRetry={handleSearch}
    />
  );
};

export default WelcomePage;
