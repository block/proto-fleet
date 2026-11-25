import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { bulkRenameModes, createDefaultBulkRenamePreferences } from "./bulkRenameDefinitions";
import BulkWorkerNameEditor from "./BulkWorkerNameEditor";

test("discards open options and unsaved edits when the editor closes", () => {
  const editor = (open: boolean) => (
    <BulkWorkerNameEditor
      open={open}
      preferences={createDefaultBulkRenamePreferences(bulkRenameModes.worker)}
      previewMiners={[]}
      previewRows={[]}
      applyLabel="Apply to 1 miner"
      onPreferencesChange={vi.fn()}
      onApply={vi.fn()}
      onDismiss={vi.fn()}
    />
  );
  const { rerender } = render(editor(true));
  fireEvent.click(screen.getByRole("button", { name: "Edit Custom options" }));
  const initialOptions = screen.getByRole("dialog", { name: "Options" });
  const prefix = within(initialOptions).getByRole("textbox", { name: "Prefix (optional)" });
  fireEvent.change(prefix, { target: { value: "unsaved-" } });
  expect(prefix).toHaveValue("unsaved-");

  rerender(editor(false));
  rerender(editor(true));

  expect(screen.queryByRole("dialog", { name: "Options" })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Edit Custom options" }));
  const reopenedOptions = screen.getByRole("dialog", { name: "Options" });
  expect(within(reopenedOptions).getByRole("textbox", { name: "Prefix (optional)" })).toHaveValue("");
});
