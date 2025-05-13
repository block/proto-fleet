import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import { Network, SetupHeader } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const NetworkPage = () => {
  const { data: networkInfo } = useNetworkInfo();
  const navigate = useNavigate();

  const handleSubmit = (networkName: string) => {
    void networkName;
    // TODO updateNetworkInfo is not implemented yet
    navigate("/onboarding/mining-pool");
    return;
    /*const networkUpdateRequest = create(UpdateNetworkNicknameRequestSchema, {
      networkNickname: networkName,
    });
    updateNetworkInfo({
      networkUpdateRequest: networkUpdateRequest,
      onSuccess: () => navigate("/onboarding/authentication"),
    });*/
  };

  return (
    <div>
      <SetupHeader />
      <Network
        submit={handleSubmit}
        // TODO no network name
        networkName={networkInfo?.networkNickname || "Pending"}
        ipRange={networkInfo?.subnet || "Pending"}
        gateway={networkInfo?.gateway || "Pending"}
      />
    </div>
  );
};

export default NetworkPage;
