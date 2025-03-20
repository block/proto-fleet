import AlertStatus from "./AlertStatus";
import BankBalance from "./BankBalance";
import BitcoinExchangeRate from "./BitcoinExchangeRate";
import LocationSelector from "./LocationSelector";

const PageHeader = () => {
  return (
    <div className="h-[60px] flex border-b border-border-5 items-center">
      <div className="flex grow px-4 items-center">
        <div className="flex grow">
          <LocationSelector />
        </div>
        <div className="flex space-x-3">
          <AlertStatus />
          <BankBalance />
          <BitcoinExchangeRate />
        </div>
      </div>
    </div>
  );
};

export default PageHeader;
