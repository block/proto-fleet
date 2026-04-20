import BankBalanceComponent from "./BankBalance";

interface BankBalanceArgs {
  loading: boolean;
  balance: number;
}

export const BankBalance = ({ loading, balance }: BankBalanceArgs) => {
  return <BankBalanceComponent loading={loading} balance={balance} />;
};

export default {
  title: "Proto Fleet/Page Header/Bank Balance",
  args: {
    loading: false,
    balance: 1630,
  },
  argTypes: {
    balance: { control: { type: "number", min: 0 } },
  },
};
