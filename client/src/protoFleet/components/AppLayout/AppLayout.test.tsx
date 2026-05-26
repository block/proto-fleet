import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppLayout from "./AppLayout";
import type { UseSchedulePillDataResult } from "@/protoFleet/components/PageHeader/useSchedulePillData";

const mockUseWindowDimensions = vi.fn();
const mockUseReactiveLocalStorage = vi.fn();
const mockUseSchedulePillData = vi.fn();

vi.mock("@/protoFleet/api/ScheduleApiProvider", () => ({
  ScheduleApiProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoFleet/components/NavigationMenu", () => ({
  __esModule: true,
  default: () => <div>Navigation menu</div>,
}));

vi.mock("@/protoFleet/components/PageHeader", () => ({
  __esModule: true,
  default: () => <div>Page header</div>,
}));

vi.mock("@/protoFleet/components/PageHeader/useSchedulePillData", () => ({
  useSchedulePillData: () => mockUseSchedulePillData(),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockUseWindowDimensions(),
}));

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: () => mockUseReactiveLocalStorage(),
}));

const createSchedulePillData = (overrides: Partial<UseSchedulePillDataResult> = {}): UseSchedulePillDataResult => ({
  hasVisibleSchedules: false,
  pillSchedule: null,
  sections: [],
  pendingScheduleId: null,
  onToggleScheduleStatus: vi.fn(),
  ...overrides,
});

describe("AppLayout", () => {
  beforeEach(() => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: true,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
    mockUseSchedulePillData.mockReturnValue(createSchedulePillData());
  });

  it("offsets the phone content when schedules make the header widgets visible", () => {
    mockUseSchedulePillData.mockReturnValue(
      createSchedulePillData({
        hasVisibleSchedules: true,
      }),
    );

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+57px)]");
  });
});
