import { MemoryRouter } from "react-router-dom";
import { fireEvent, render } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import AlertStatus from "./AlertStatus";
import { alerts } from "@/protoFleet/features/fleetManagement/components/AlertsModal/stories/mocks";

describe("Alert Status", () => {
  const alertIconTestId = "alert-icon";
  const modalTestId = "modal";

  const warningClass = "text-text-warning";

  test("does not render when there are no alerts", () => {
    const { container } = render(<AlertStatus loading={false} alerts={[]} />);

    expect(container.firstChild).toBeNull();
  });

  test("renders alert when there are some alerts", () => {
    const { getByTestId } = render(
      <AlertStatus loading={false} alerts={alerts} />,
    );

    expect(getByTestId(alertIconTestId)).toHaveClass(warningClass);
  });

  test("does not render icon when loading", () => {
    const { queryByTestId } = render(
      <AlertStatus loading={true} alerts={alerts} />,
    );

    expect(queryByTestId(alertIconTestId)).toBeNull();
  });

  test("opens modal when status chip is clicked", () => {
    const { queryByTestId, getByTestId, getByText } = render(
      <MemoryRouter>
        <AlertStatus loading={false} alerts={alerts} />
      </MemoryRouter>,
    );

    expect(queryByTestId(modalTestId)).not.toBeInTheDocument();

    fireEvent.click(getByText(`${alerts.length} Alerts`));

    expect(getByTestId(modalTestId)).toBeInTheDocument();
  });
});
