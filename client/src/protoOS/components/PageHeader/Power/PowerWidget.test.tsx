import { BrowserRouter } from "react-router-dom";
import { fireEvent, render } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import PowerWidget from "./PowerWidget";
import { useIsAwake, useIsSleeping } from "@/protoOS/store";
import { PopoverProvider } from "@/shared/components/Popover";

vi.mock("@/protoOS/store", async (importOriginal) => {
  const actual = (await importOriginal()) as any;
  return {
    ...actual,
    useIsAwake: vi.fn(() => true),
    useIsSleeping: vi.fn(() => false),
  };
});

vi.mock("@/protoOS/features/auth/contexts/AuthContext", () => ({
  AUTH_ACTIONS: {
    reboot: "reboot",
    sleep: "sleep",
  },
  useAccessToken: vi.fn(() => ({
    checkAccess: vi.fn(),
    hasAccess: false,
    setHasAccess: vi.fn(),
  })),
  useAuthContext: vi.fn(() => ({
    dismissedLoginModal: false,
    setDismissedLoginModal: vi.fn(),
    pausedAuthAction: null,
    setPausedAuthAction: vi.fn(),
  })),
}));

describe("Power Widget", () => {
  const powerButton = "power-button";
  const powerPopover = "power-popover";
  const popoverRebootButton = "popover-reboot-button";
  const popoverSleepButton = "popover-sleep-button";
  const popoverWakeUpButton = "popover-wake-up-button";

  const PowerWidgetProps = {
    onReboot: vi.fn(),
    onSleep: vi.fn(),
    onWake: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    (useIsAwake as Mock).mockReturnValue(true);
    (useIsSleeping as Mock).mockReturnValue(false);
  });

  test("renders power widget popover with reboot and sleep if miner is running", () => {
    (useIsAwake as Mock).mockReturnValue(true);

    const { getByTestId, queryByTestId } = render(
      <BrowserRouter>
        <PopoverProvider>
          <PowerWidget {...PowerWidgetProps} />
        </PopoverProvider>
      </BrowserRouter>,
    );
    const buttonElement = getByTestId(powerButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(powerPopover)).toBeInTheDocument();
    expect(queryByTestId(popoverRebootButton)).toBeInTheDocument();
    expect(queryByTestId(popoverSleepButton)).toBeInTheDocument();
    expect(queryByTestId(popoverWakeUpButton)).not.toBeInTheDocument();
  });

  test("renders power widget popover with reboot and wake up if miner is stopped", () => {
    (useIsAwake as Mock).mockReturnValue(false);

    const { getByTestId, queryByTestId } = render(
      <BrowserRouter>
        <PopoverProvider>
          <PowerWidget {...PowerWidgetProps} />
        </PopoverProvider>
      </BrowserRouter>,
    );
    const buttonElement = getByTestId(powerButton);
    fireEvent.click(buttonElement);

    expect(getByTestId(powerPopover)).toBeInTheDocument();
    expect(getByTestId(popoverRebootButton)).toBeInTheDocument();
    expect(getByTestId(popoverWakeUpButton)).toBeInTheDocument();
    expect(queryByTestId(popoverSleepButton)).not.toBeInTheDocument();
  });

  test("closes popover on click of reboot", () => {
    const { getByTestId, queryByTestId } = render(
      <BrowserRouter>
        <PopoverProvider>
          <PowerWidget shouldShowPopover {...PowerWidgetProps} />
        </PopoverProvider>
      </BrowserRouter>,
    );
    const buttonElement = getByTestId(popoverRebootButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });

  test("closes popover on click of sleep", () => {
    const { getByTestId, queryByTestId } = render(
      <BrowserRouter>
        <PopoverProvider>
          <PowerWidget shouldShowPopover {...PowerWidgetProps} />
        </PopoverProvider>
      </BrowserRouter>,
    );
    const buttonElement = getByTestId(popoverSleepButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });

  test("closes popover on click of wake up", () => {
    (useIsAwake as Mock).mockReturnValue(false);

    const { getByTestId, queryByTestId } = render(
      <BrowserRouter>
        <PopoverProvider>
          <PowerWidget shouldShowPopover {...PowerWidgetProps} />
        </PopoverProvider>
      </BrowserRouter>,
    );
    const buttonElement = getByTestId(popoverWakeUpButton);
    fireEvent.click(buttonElement);
    expect(queryByTestId(powerPopover)).not.toBeInTheDocument();
  });
});
