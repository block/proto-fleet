import {
  MemoryRouter,
  NavigateFunction,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TabMenu from "./TabMenu"; // Adjust the import path as necessary
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useLocation: vi.fn(),
    useNavigate: vi.fn(),
  };
});

let location = "/tab1";
const mockUseLocation = vi.fn().mockImplementation(() => ({
  pathname: location,
  search: "",
  hash: "",
  state: null,
  key: "default",
}));

beforeEach(() => {
  // Reset and setup mocks in beforeEach
  const mockNavigate = (path: string) => {
    location = path;
    mockUseLocation.mockImplementation(() => ({
      pathname: location,
      search: "",
      hash: "",
      state: null,
      key: "default",
    }));
  };

  vi.mocked(useLocation).mockImplementation(mockUseLocation);
  vi.mocked(useNavigate).mockImplementation(
    () => mockNavigate as NavigateFunction,
  );
});

describe("TabMenu", () => {
  const items = {
    tab1: {
      name: "Tab 1",
      value: 1,
      units: "Units 1",
      path: "/tab1",
    },
    tab2: { name: "Tab 2", value: 2, units: "Units 2", path: "/tab2" },
    tab3: { name: "Tab 3", value: 3, units: "Units 3", path: "/tab3" },
  };

  it("renders all tabs", () => {
    render(
      <MemoryRouter>
        <TabMenu items={{ ...items }} />
      </MemoryRouter>,
    );
    Object.values(items).forEach((item) => {
      expect(screen.getByText(item.name)).toBeInTheDocument();
    });
  });

  it("switches active tab on click", () => {
    render(
      <MemoryRouter>
        <MinerHostingProvider>
          <TabMenu items={{ ...items }} />
        </MinerHostingProvider>
      </MemoryRouter>,
    );

    const tab1 = screen.getByText("Tab 1").closest("button");
    const tab2 = screen
      .getByText("Tab 2")
      .closest("button") as HTMLButtonElement;

    // Initially, the first tab should be active
    expect(tab1).toHaveClass("bg-surface-base");
    expect(tab2).not.toHaveClass("bg-surface-base");

    // Click on the second tab
    fireEvent.click(tab2);

    // Now, the second tab should be active
    waitFor(() => {
      expect(tab1).not.toHaveClass("bg-surface-base");
      expect(tab2).toHaveClass("bg-surface-base");
    });
  });
});
