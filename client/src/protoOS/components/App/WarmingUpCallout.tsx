import Callout, { intents } from "@/shared/components/Callout";
import ProgressCircular from "@/shared/components/ProgressCircular";

const WarmingUpCallout = () => {
  return (
    <div className="mb-10">
      <Callout
        intent={intents.default}
        prefixIcon={<ProgressCircular indeterminate />}
        title="Your miner is warming up. Once warmed up, it’ll start mining. This can take a few minutes."
      />
    </div>
  );
};

export default WarmingUpCallout;
