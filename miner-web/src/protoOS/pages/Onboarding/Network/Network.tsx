import { useNetworkInfo } from "@/protoOS/api";
import { Network } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const NetworkPage = () => {
  const { data: networkInfo } = useNetworkInfo();
  const navigate = useNavigate();

  return (
    <div>
      {/* <SetupHeader activeStep="network" /> */}
      <Network
        submit={() => {
          // TODO: Send network info to the API
          navigate("/onboarding/authentication");
        }}
        // What should we show here?
        networkName={"Pending"}
        // What should we show here?
        ipRange={networkInfo?.ip || "Pending"}
        gateway={networkInfo?.gateway || "Pending"}
      />
    </div>
  );
};

export default NetworkPage;
