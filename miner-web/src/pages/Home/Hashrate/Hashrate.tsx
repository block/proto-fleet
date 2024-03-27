import InfoWidget from "components/InfoWidget";
import HashrateChart from "./HashrateChart";

const Hashrate = () => {
  return (
    <div className="space-y-6">
      <div className="flex space-x-6 w-full">
        {/* TODO: display hashrate values once API is implemented */}
        <InfoWidget
          title="Current hashrate"
          value="230.2 TH/s"
          loading={false}
        />
        <InfoWidget title="Average" value="225.1 TH/s" loading={false} />
        <InfoWidget title="Lowest" value="215.2 TH/s" loading={false} />
        <InfoWidget title="Highest" value="231.2 TH/s" loading={false} />
      </div>

      <div className="w-[880px] h-[400px]">
        <HashrateChart />
      </div>
    </div>
  );
};

export default Hashrate;
