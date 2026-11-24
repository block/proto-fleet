import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { ConfigurationForm } from "./ConfigurationForm";
import type { ConfigurationPoolDraft } from "./types";

test("keeps both field edits queued before the parent renders again", () => {
  const pool: ConfigurationPoolDraft = {
    key: "primary",
    url: "stratum+tcp://pool.example:3333",
    username: "",
    password: "",
    preservePassword: false,
  };
  const updates: Array<(pools: ConfigurationPoolDraft[]) => ConfigurationPoolDraft[]> = [];
  render(
    <ConfigurationForm
      name="Production"
      onChangeName={() => {}}
      pools={[pool]}
      onChangePools={(update) => updates.push(update)}
      newPool={() => pool}
      validateURL={() => undefined}
    />,
  );

  fireEvent.change(screen.getByLabelText("Username"), { target: { value: "account.worker" } });
  fireEvent.change(screen.getByLabelText("Pool password (optional)"), { target: { value: "pool-password" } });

  expect(updates.reduce((pools, update) => update(pools), [pool])).toEqual([
    { ...pool, username: "account.worker", password: "pool-password" },
  ]);
});
