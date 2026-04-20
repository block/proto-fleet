import { render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import BitcoinExchangeRate from "./BitcoinExchangeRate";

describe("Bank Balance", () => {
  const bitcoinIconTestId = "bitcoin-icon";
  const skeletonTestId = "skeleton-bar";

  const usdCurrency = "$";

  test("renders loading state", () => {
    const { getByTestId } = render(<BitcoinExchangeRate loading={true} exchangeRate={0} />);

    expect(getByTestId(bitcoinIconTestId)).toBeDefined();
    expect(getByTestId(skeletonTestId)).toBeDefined();
  });

  test("renders exchange rate with currency", () => {
    const { queryByText, getByTestId, queryByTestId } = render(
      <BitcoinExchangeRate loading={false} exchangeRate={0} />,
    );

    expect(getByTestId(bitcoinIconTestId)).toBeDefined();
    expect(queryByTestId(skeletonTestId)).toBeNull();
    expect(queryByText(usdCurrency, { exact: false })).toBeInTheDocument();
  });
});
