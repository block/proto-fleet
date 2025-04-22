import BankBalance from "./BankBalance";

const BankBalanceWrapper = () => {
  // TODO load balance from API
  const balance = 1630;
  const loading = false;

  return <BankBalance balance={balance} loading={loading} />;
};

export default BankBalanceWrapper;
