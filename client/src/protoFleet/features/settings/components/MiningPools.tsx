import MiningPoolsForm from "@/protoFleet/components/MiningPools";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MiningPools = () => {
  return (
    <div className="flex flex-col gap-6">
      <Header title="Pools" titleSize="text-heading-300" />
      <MiningPoolsForm
        buttonLabel="Continue"
        onSaveDone={() =>
          pushToast({
            message: "Your mining pools have been saved",
            status: STATUSES.success,
          })
        }
        onSaveFailed={() =>
          pushToast({
            message: "Something went wrong, please try again",
            status: STATUSES.error,
          })
        }
      />
    </div>
  );
};

export default MiningPools;
