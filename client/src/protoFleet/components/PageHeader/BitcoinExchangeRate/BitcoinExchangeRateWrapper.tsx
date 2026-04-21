import BitcoinExchangeRate from "./BitcoinExchangeRate";

const BitcoinExchangeRateWrapper = () => {
  const exchangeRate = 89729.88;
  const loading = false;

  return <BitcoinExchangeRate exchangeRate={exchangeRate} loading={loading} />;
};

export default BitcoinExchangeRateWrapper;
