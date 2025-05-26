import MiningPoolsForm from "@/protoFleet/components/MiningPools";
import Header from "@/shared/components/Header";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const MiningPools = () => {
  return (
    <div className="mx-auto flex max-w-xl flex-col gap-6">
      <Header
        title={"Update your mining pools"}
        titleSize="text-heading-300"
        description={"TODO - add description"}
      />
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
