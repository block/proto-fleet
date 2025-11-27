import { bitcoinCurrency } from "./constants";
import { BankAccount } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Chip from "@/shared/components/Chip";
import SkeletonBar from "@/shared/components/SkeletonBar";

import { getDisplayValue } from "@/shared/utils/stringUtils";

interface BankBalanceProps {
  balance?: number;
  loading?: boolean;
}

const BankBalance = ({ balance, loading }: BankBalanceProps) => {
  const formattedBalance = () => {
    if (balance === undefined || balance === null) return;

    if (balance > 1000) return getDisplayValue(balance / 1000) + "k";
    return getDisplayValue(balance);
  };

  return (
    <Chip prefixIcon={<BankAccount width={iconSizes.small} />}>
      {loading ? <SkeletonBar className="w-16" /> : <>{bitcoinCurrency + formattedBalance()}</>}
    </Chip>
  );
};

export default BankBalance;
