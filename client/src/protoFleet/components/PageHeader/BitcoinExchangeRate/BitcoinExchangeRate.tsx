import { useMemo } from "react";
import { Bitcoin } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Chip from "@/shared/components/Chip";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface BitcoinExchangeRateProps {
  exchangeRate?: number;
  loading?: boolean;
}

const BitcoinExchangeRate = ({ exchangeRate, loading }: BitcoinExchangeRateProps) => {
  const formattedRate = useMemo(() => {
    if (exchangeRate === undefined || exchangeRate === null) return;

    return new Intl.NumberFormat("en-US", {
      style: "currency",
      currency: "USD",
    }).format(exchangeRate);
  }, [exchangeRate]);

  return (
    <Chip prefixIcon={<Bitcoin width={iconSizes.small} />}>
      {loading ? <SkeletonBar className="w-16" /> : <>{formattedRate}</>}
    </Chip>
  );
};

export default BitcoinExchangeRate;
