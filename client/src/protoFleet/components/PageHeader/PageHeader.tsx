import AlertStatus from "./AlertStatus";
import BankBalance from "./BankBalance";
import BitcoinExchangeRate from "./BitcoinExchangeRate";
import LocationSelector from "./LocationSelector";

const PageHeader = () => {
  return (
    <div className="flex h-[60px] items-center border-b border-border-5">
      <div className="flex grow items-center px-4">
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
