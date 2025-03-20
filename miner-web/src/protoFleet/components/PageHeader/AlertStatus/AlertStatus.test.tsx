import { render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import AlertStatus from "./AlertStatus";

describe("Alert Status", () => {
  const alertIconTestId = "alert";
  const checkmarkIconTestId = "checkmark";

  const successClass = "text-text-success";
  const warningClass = "text-text-warning";

  test("renders checkmark when there are no alerts", () => {
    const { getByTestId } = render(
      <AlertStatus loading={false} alertsCount={0} />,
    );

    expect(getByTestId(checkmarkIconTestId)).toHaveClass(successClass);
  });

  test("renders alert when there are some alerts", () => {
    const { getByTestId } = render(
      <AlertStatus loading={false} alertsCount={10} />,
    );

    expect(getByTestId(alertIconTestId)).toHaveClass(warningClass);
  });

  test("does not render icon when loading", () => {
    const { queryByTestId } = render(
      <AlertStatus loading={true} alertsCount={undefined} />,
    );

    expect(queryByTestId(checkmarkIconTestId)).toBeNull();
    expect(queryByTestId(alertIconTestId)).toBeNull();
  });
});
