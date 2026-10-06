import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, test } from "vitest";

import ChatFab from "./ChatFab";
import { useChatStore } from "./useChatStore";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

beforeEach(() => {
  useChatStore.getState().resetSession();
  useFleetStore.getState().ui.setActionBarVisible(false);
});

test("keeps the launcher above bulk actions and still opens chat", () => {
  render(<ChatFab />);
  act(() => useFleetStore.getState().ui.setActionBarVisible(true));
  const launcher = screen.getByRole("button", { name: "Open Minerbot" });
  expect(launcher).toHaveClass("bottom-24", "phone:bottom-28");
  fireEvent.click(launcher);
  expect(useChatStore.getState().isOpen).toBe(true);
});
