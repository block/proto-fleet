import { useMinerHardware, useOnboarded, useProductName } from "@/protoOS/store";
import { WelcomeScreen } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const Welcome = () => {
  const navigate = useNavigate();
  const isOnboarded = useOnboarded();
  const productName = useProductName();
  const minerHardware = useMinerHardware();

  // Bootstrap is complete when critical initial data has loaded:
  // - System status (isOnboarded !== undefined)
  // - System info (productName exists)
  // - Hardware data (miner hardware exists)
  const isBootstrapComplete = isOnboarded !== undefined && productName !== undefined && minerHardware !== undefined;

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
      isBootstrapComplete={isBootstrapComplete}
    />
  );
};

export default Welcome;
