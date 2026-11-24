import { fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import Onboarding from "./Onboarding";
import { MinerHostingProvider, useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

const AdapterState = () => <span data-testid="adapter-state">{useMinerHosting().resolution.status}</span>;

vi.mock("react-router-dom", async (importOriginal) => {
  const actual = (await importOriginal()) as any;
  return {
    ...actual,
    useNavigate: () => ({
      Navigation: vi.fn(),
    }),
    useLocation: () => ({
      pathname: "/onboarding",
    }),
    Link: vi.fn(),
  };
});

const poolUrl = "stratum+tcp://ckpool:3333";

// data-testid constants for new unified pool list UI
const addPoolButton = "add-pool-button";
const addAnotherPoolButton = "add-another-pool-button";
const poolSaveButton = "pool-save-button";
const finishSetupButton = "finish-setup-button";
const warnDefaultPoolCallout = "warn-default-pool-callout";
const warnBackupPoolDialog = "warn-backup-pool-dialog";

// Modal inputs use poolIndex in their testIds
const getPoolNameInput = (poolIndex: number) => `pool-name-${poolIndex}-input`;
const getUrlInput = (poolIndex: number) => `url-${poolIndex}-input`;
const getUsernameInput = (poolIndex: number) => `username-${poolIndex}-input`;
const getEditButton = (poolIndex: number) => `pool-${poolIndex}-edit-button`;

describe("Onboarding", () => {
  let component: ReturnType<typeof render>;
  let getByTestId: typeof component.getByTestId;
  let queryByTestId: typeof component.queryByTestId;

  beforeEach(async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>((input) =>
        Promise.resolve(
          String(input).endsWith("/api/v1/system")
            ? new Response(JSON.stringify({ "system-info": {} }), { status: 200 })
            : new Response(null, { status: 404 }),
        ),
      ),
    );
    component = render(
      <MinerHostingProvider>
        <AdapterState />
        <Onboarding />
      </MinerHostingProvider>,
    );
    getByTestId = component.getByTestId;
    queryByTestId = component.queryByTestId;
    await waitFor(() => expect(getByTestId("adapter-state")).toHaveTextContent("ready"));
  });

  afterEach(() => vi.unstubAllGlobals());

  const addPool = async (
    user: ReturnType<typeof userEvent.setup>,
    poolIndex: number,
    name: string,
    url: string,
    username: string,
  ) => {
    await user.click(getByTestId(poolIndex === 0 ? addPoolButton : addAnotherPoolButton));
    await waitFor(() => {
      expect(getByTestId(getUrlInput(poolIndex))).toBeInTheDocument();
    });

    const poolNameInput = getByTestId(getPoolNameInput(poolIndex));
    const urlInput = getByTestId(getUrlInput(poolIndex));
    const usernameInput = getByTestId(getUsernameInput(poolIndex));
    await user.clear(poolNameInput);
    await user.type(poolNameInput, name);
    await user.clear(urlInput);
    await user.type(urlInput, url);
    await user.clear(usernameInput);
    await user.type(usernameInput, username);
    await user.click(getByTestId(poolSaveButton));

    await waitFor(() => {
      expect(getByTestId(getEditButton(poolIndex))).toBeInTheDocument();
    });
  };

  test("Renders callout on clicking finish setup with no pools configured", () => {
    // callout has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
    fireEvent.click(getByTestId(finishSetupButton));
    // callout no longer has max-height of 0
    expect(getByTestId(warnDefaultPoolCallout)).not.toHaveClass("max-h-0");
  });

  test("Renders the backup pool warning dialog, and no default pool callout, on clicking finish setup with one pool", async () => {
    const user = userEvent.setup();

    await addPool(user, 0, "Test Pool", poolUrl, "testuser");

    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");

    await user.click(getByTestId(finishSetupButton));

    await waitFor(() => {
      expect(getByTestId(warnBackupPoolDialog)).toBeInTheDocument();
    });
    expect(getByTestId(warnDefaultPoolCallout)).toHaveClass("max-h-0");
  });

  test("Does not render warning dialog on clicking finish setup with backup pool configured", async () => {
    const user = userEvent.setup();

    await addPool(user, 0, "Primary Pool", poolUrl, "testuser");
    await addPool(user, 1, "Backup Pool", "stratum+tcp://backup:3333", "backupuser");

    // The two pools show their priority numbers.
    expect(component.getByText("1")).toBeInTheDocument();
    expect(component.getByText("2")).toBeInTheDocument();

    await user.click(getByTestId(finishSetupButton));

    expect(queryByTestId(warnBackupPoolDialog)).not.toBeInTheDocument();
  });
});
