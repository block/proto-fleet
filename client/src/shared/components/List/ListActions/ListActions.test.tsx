import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import ListActions from "./ListActions";
import { PopoverProvider } from "@/shared/components/Popover";

describe("List actions", () => {
  const item = {
    message: "Hashboard error",
    alertType: "hashboard",
  };

  const actions = [
    {
      title: "Archive",
      actionHandler: vi.fn(),
    },
    {
      title: "View miner",
      actionHandler: vi.fn(),
    },
    {
      title: "Reboot miner",
      actionHandler: vi.fn(),
    },
  ];

  afterEach(() => {
    vi.restoreAllMocks();
  });

  test("renders with provided actions", () => {
    render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = screen.getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    actions.forEach((action) => {
      expect(screen.getByText(action.title)).toBeInTheDocument();
    });
  });

  test("hides actions on click outside", async () => {
    render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = screen.getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    actions.forEach((action) => {
      expect(screen.getByText(action.title)).toBeInTheDocument();
    });

    // simulate click outside
    fireEvent.mouseDown(document);
    await waitFor(() => {
      actions.forEach((action) => {
        expect(screen.queryByText(action.title)).not.toBeInTheDocument();
      });
    });
  });

  test("calls onAction callback when an action is clicked", () => {
    render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = screen.getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    fireEvent.click(screen.getByText(actions[0].title));
    expect(actions[0].actionHandler).toHaveBeenCalledWith(item);

    // Popover closes after click, so we need to reopen it to test another action
    fireEvent.click(triggerButton);
    fireEvent.click(screen.getByText(actions[1].title));
    expect(actions[1].actionHandler).toHaveBeenCalledWith(item);
  });

  test("does not break when no actions are provided", () => {
    const { queryByTestId } = render(
      <PopoverProvider>
        <ListActions item={item} actions={[]} />,
      </PopoverProvider>,
    );
    const triggerButton = queryByTestId("list-actions-trigger");
    expect(triggerButton).not.toBeInTheDocument();
  });

  test("positions the popover using the current scroll offset when opened after scrolling", async () => {
    Object.defineProperty(window, "scrollY", {
      value: 500,
      configurable: true,
    });

    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function mockRect(this: HTMLElement) {
      if (this.className === "relative") {
        return {
          x: 120,
          y: 40,
          width: 32,
          height: 32,
          top: 40,
          left: 120,
          bottom: 72,
          right: 152,
          toJSON: () => ({}),
        } as DOMRect;
      }

      if (typeof this.className === "string" && this.className.includes("z-50")) {
        return {
          x: 0,
          y: 0,
          width: 240,
          height: 160,
          top: 0,
          left: 0,
          bottom: 160,
          right: 240,
          toJSON: () => ({}),
        } as DOMRect;
      }

      return {
        x: 0,
        y: 0,
        width: 0,
        height: 0,
        top: 0,
        left: 0,
        bottom: 0,
        right: 0,
        toJSON: () => ({}),
      } as DOMRect;
    });

    render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />
      </PopoverProvider>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));

    const popoverContainer = await waitFor(() => {
      const actionRow = screen.getByText("Archive");
      const container = actionRow.closest(".popover-content")?.parentElement as HTMLDivElement | null;

      if (container === null) {
        throw new Error("Expected popover container to be rendered");
      }

      return container;
    });

    expect(popoverContainer.style.top).toBe("580px");
    expect(popoverContainer.style.left).toBe("8px");
  });

  test("supports item-aware action labels and keeps disabled actions inert", () => {
    const dynamicActions = [
      {
        title: (currentItem: typeof item) => `Pause ${currentItem.alertType}`,
        actionHandler: vi.fn(),
      },
      {
        title: "Hidden action",
        hidden: true,
        actionHandler: vi.fn(),
      },
      {
        title: "Disabled action",
        disabled: true,
        actionHandler: vi.fn(),
      },
    ];

    render(
      <PopoverProvider>
        <ListActions item={item} actions={dynamicActions} />
      </PopoverProvider>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));

    expect(screen.getByText("Pause hashboard")).toBeInTheDocument();
    expect(screen.queryByText("Hidden action")).not.toBeInTheDocument();
    expect(screen.getByText("Disabled action")).toBeInTheDocument();

    fireEvent.click(screen.getByText("Disabled action"));
    expect(dynamicActions[2].actionHandler).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("Pause hashboard"));
    expect(dynamicActions[0].actionHandler).toHaveBeenCalledWith(item);
  });

  test("uses the critical fill token for destructive menu actions", () => {
    const destructiveActions = [
      {
        title: "Deactivate",
        variant: "destructive" as const,
        actionHandler: vi.fn(),
      },
    ];

    render(
      <PopoverProvider>
        <ListActions item={item} actions={destructiveActions} />
      </PopoverProvider>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));

    expect(screen.getByText("Deactivate")).toHaveClass("text-intent-critical-fill");
  });

  test("allows actions to suppress the divider after a specific row", () => {
    const dividerActions = [
      {
        title: "Edit",
        showDividerAfter: false,
        actionHandler: vi.fn(),
      },
      {
        title: "Pause",
        actionHandler: vi.fn(),
      },
      {
        title: "Delete",
        actionHandler: vi.fn(),
      },
    ];

    render(
      <PopoverProvider>
        <ListActions item={item} actions={dividerActions} />
      </PopoverProvider>,
    );

    fireEvent.click(screen.getByTestId("list-actions-trigger"));

    const editRow = screen.getByText("Edit").closest("button")?.parentElement;
    const pauseRow = screen.getByText("Pause").closest("button")?.parentElement;

    expect(editRow?.querySelector("div.mt-\\[-1px\\]")).not.toBeInTheDocument();
    expect(pauseRow?.querySelector("div.mt-\\[-1px\\]")).toBeInTheDocument();
  });
});
