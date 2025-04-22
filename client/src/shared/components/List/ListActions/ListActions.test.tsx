import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
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

  test("renders with provided actions", () => {
    const { getByText, getByTestId } = render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    actions.forEach((action) => {
      expect(getByText(action.title)).toBeInTheDocument();
    });
  });

  test("hides actions on click outside", async () => {
    const { getByText, getByTestId, queryByText } = render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    actions.forEach((action) => {
      expect(getByText(action.title)).toBeInTheDocument();
    });

    // simulate click outside
    fireEvent.mouseDown(document);
    await waitFor(() => {
      actions.forEach((action) => {
        expect(queryByText(action.title)).not.toBeInTheDocument();
      });
    });
  });

  test("calls onAction callback when an action is clicked", () => {
    const { getByText, getByTestId } = render(
      <PopoverProvider>
        <ListActions item={item} actions={actions} />,
      </PopoverProvider>,
    );
    const triggerButton = getByTestId("list-actions-trigger");
    expect(triggerButton).toBeInTheDocument();
    fireEvent.click(triggerButton);

    fireEvent.click(getByText(actions[0].title));
    expect(actions[0].actionHandler).toHaveBeenCalledWith(item);

    fireEvent.click(getByText(actions[1].title));
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
});
