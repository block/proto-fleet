import { durations } from "components/DurationSelector";

import HashrateComponent from ".";

export const Hashrate = () => {
  return (
    <div className="flex justify-center my-8">
      <div className="w-[928px]">
        <HashrateComponent duration={durations[0]} />
      </div>
    </div>
  );
};

export default {
  title: "pages/Home/Hashrate",
};
