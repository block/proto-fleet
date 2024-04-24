import InfoWidget from "components/InfoWidget";
import HashrateChart from "./HashrateChart";

const Hashrate = () => {
  return (
    <div className="space-y-6">
      <div className="flex space-x-6 w-full phone:flex-col phone:space-x-0 phone:space-y-6">
        {/* TODO: display hashrate values once API is implemented */}
        <InfoWidget
          title="Current hashrate"
          value="105.2 TH/s"
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Average"
          value="106.1 TH/s"
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Lowest"
          value="101.2 TH/s"
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
        <InfoWidget
          title="Highest"
          value="110.2 TH/s"
          loading={false}
          wrapperClassName="w-full tablet:w-32"
        />
      </div>

      <div className="h-[400px]">
        <HashrateChart />
      </div>
    </div>
  );
};

export default Hashrate;
