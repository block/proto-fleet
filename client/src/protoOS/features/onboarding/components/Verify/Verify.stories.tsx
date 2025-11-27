import { action } from "storybook/actions";
import VerifyComponent from "./Verify";

export const Verify = () => {
  const miner = {
    macAddress: "0d:04:8a:54:fa:00",
    serialNumber: "0123456789",
  };

  return (
    <div className="mx-auto max-w-[800px]">
      <VerifyComponent miner={miner} handleContinueSetup={action("continue setup")} />
    </div>
  );
};

export default {
  title: "ProtoOS/Onboarding/Verify",
};
