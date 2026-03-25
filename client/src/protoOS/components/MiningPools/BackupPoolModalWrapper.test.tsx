import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import BackupPoolModalWrapper from "./BackupPoolModalWrapper";

vi.mock("@/protoOS/api", () => ({
  useTestConnection: () => ({
    pending: false,
    testConnection: vi.fn(),
  }),
}));

describe("BackupPoolModalWrapper", () => {
  it("shows worker-name guidance and allows saving without a username", () => {
    render(
      <BackupPoolModalWrapper
        open
        onChangePools={vi.fn()}
        onDismiss={vi.fn()}
        poolIndex={1}
        pools={[
          {
            name: "Default Pool",
            url: "stratum+tcp://pool.example.com:3333",
            username: "default-user",
            password: "",
            priority: 0,
          },
          {
            name: "Backup Pool",
            url: "stratum+tcp://backup.example.com:3333",
            username: "",
            password: "",
            priority: 1,
          },
          {
            name: "",
            url: "",
            username: "",
            password: "",
            priority: 2,
          },
        ]}
      />,
    );

    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    const usernameInput = screen.getByLabelText("Username (optional)");
    const helperText = usernameInput.closest(".space-y-2")?.querySelector(".text-200.text-text-primary-70");

    expect(usernameInput).toBeInTheDocument();
    expect(helperText).toHaveTextContent(
      "To add a worker name, add a period after the username followed by the worker name.Example: mann23.workerbee",
    );
  });
});
