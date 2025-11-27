import { render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import BankBalance from "./BankBalance";
import { bitcoinCurrency } from "./constants";

describe("Bank Balance", () => {
  const bankIconTestId = "bank-account-icon";
  const skeletonTestId = "skeleton-bar";

  test("renders loading state", () => {
    const { getByTestId } = render(<BankBalance loading={true} balance={0} />);

    expect(getByTestId(bankIconTestId)).toBeDefined();
    expect(getByTestId(skeletonTestId)).toBeDefined();
  });

  test("renders balance with currency", () => {
    const { queryByText, getByTestId, queryByTestId } = render(<BankBalance loading={false} balance={0} />);

    expect(getByTestId(bankIconTestId)).toBeDefined();
    expect(queryByTestId(skeletonTestId)).toBeNull();
    expect(queryByText(bitcoinCurrency, { exact: false })).toBeInTheDocument();
  });
});
