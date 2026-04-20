import BitcoinExchangeRateComponent from "./BitcoinExchangeRate";

interface BitcoinExchangeRateArgs {
  loading: boolean;
  exchangeRate: number;
}

export const BitcoinExchangeRate = ({ loading, exchangeRate }: BitcoinExchangeRateArgs) => {
  return <BitcoinExchangeRateComponent loading={loading} exchangeRate={exchangeRate} />;
};

export default {
  title: "Proto Fleet/Page Header/Bitcoin Exchange Rate",
  args: {
    loading: false,
    exchangeRate: 89729.88,
  },
  argTypes: {
    exchangeRate: { control: { type: "number", min: 0 } },
  },
};
