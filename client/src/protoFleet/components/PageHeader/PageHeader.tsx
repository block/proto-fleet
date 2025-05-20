import { useState } from "react";
import AlertStatus from "./AlertStatus";
import BankBalance from "./BankBalance";
import BitcoinExchangeRate from "./BitcoinExchangeRate";
import LocationSelector from "./LocationSelector";
import NavigationMenu from "@/protoFleet/components/NavigationMenu";
import routes from "@/protoFleet/routes";
import { Menu } from "@/shared/assets/icons";

const PageHeader = () => {
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  return (
    <>
      {isMenuOpen && (
        <div
          className="absolute z-10 w-full bg-black/25"
          onClick={() => setIsMenuOpen(false)}
        >
          <div
            onClick={(e) => {
              e.stopPropagation();
            }}
            className="w-fit bg-white"
          >
            <NavigationMenu routes={routes} />
          </div>
        </div>
      )}
      <div className="flex w-full border-b border-border-5 max-sm:flex-col sm:h-[60px] sm:items-center">
        <div className="flex grow items-center gap-5 px-4 max-sm:border-b max-sm:border-border-5 max-sm:py-3">
          <div className="sm:hidden">
            <button
              className="flex items-center justify-center"
              onClick={() => setIsMenuOpen((prev) => !prev)}
            >
              <Menu />
            </button>
          </div>
          <div className="flex grow">
            <LocationSelector />
          </div>
          <div className="flex gap-3">
            <AlertStatus />
            <div className="flex gap-3 max-sm:hidden">
              <BankBalance />
              <BitcoinExchangeRate />
            </div>
          </div>
        </div>
        <div className="px-4 py-3 sm:hidden">
          <div className="flex gap-3">
            <BankBalance />
            <BitcoinExchangeRate />
          </div>
        </div>
      </div>
    </>
  );
};

export default PageHeader;
