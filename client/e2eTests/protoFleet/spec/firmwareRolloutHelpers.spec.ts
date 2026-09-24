import { expect, type Page, test } from "@playwright/test";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

const channel = "Helper fixture";
const target = { manufacturer: "Proto", model: "Rig" };
const version = "3.2.123";
const responsiveModalActions = `
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <style>
    [data-testid="save-channel-mobile"] { display: none; }
    @media (max-width: 631px) {
      [data-testid="save-channel"] { display: none; }
      [data-testid="save-channel-mobile"] { display: inline-block; }
    }
  </style>
`;

const modalSaveButtons = (label: string, onClick = "") =>
  ["save-channel", "save-channel-mobile"]
    .map((testId) => `<button data-testid="${testId}" onclick="${onClick}">${label}</button>`)
    .join("");

// Exercise the real page-object locators against the table structure used by
// the UI, including misleading text in other cells. No backend state is used.
test.use({ storageState: { cookies: [], origins: [] } });

async function renderTables(page: Page, historyRows: string, minerRows = "", modelStatus = "") {
  await page.setContent(`
    ${responsiveModalActions}
    <section data-testid="release-channel-${channel}">
      <button data-testid="channel-history" onclick="document.querySelector('#modals').innerHTML = document.querySelector('#history').innerHTML">History</button>
      <table><tbody><tr data-testid="model-group-Rig">
        <td>Proto Rig</td>
        <td>${modelStatus}</td>
        <td><button onclick="document.querySelector('#modals').innerHTML = document.querySelector('#miners').innerHTML">View miners</button></td>
      </tr></tbody></table>
    </section>
    <div id="modals"></div>
    <template id="miners"><section data-testid="modal">
      <h2 class="heading">Proto Rig miners</h2>
      <table><tbody>${minerRows}</tbody></table>
      <button onclick="this.closest('section').remove()">Done</button>
    </section></template>
    <template id="history"><section data-testid="modal">
      <h2 class="heading">Update history</h2>
      <table>
        <thead><tr><th>Status</th><th>Manufacturer / model</th><th>Firmware</th><th>Actions</th></tr></thead>
        <tbody>${historyRows}</tbody>
      </table>
      <button onclick="this.closest('section').remove()">Done</button>
    </section></template>
  `);
}

const completed = `<tr data-testid="history-row-2"><td>Completed</td><td>Proto Rig</td><td>${version}</td><td></td></tr>`;
const miner = (id: number, firmware: string) =>
  `<tr data-testid="channel-miner-${id}"><td>Miner ${id} ${version}</td><td>${firmware}</td><td>Up to date</td></tr>`;
const rolloutProgress = (status = "Updating") =>
  `<span role="progressbar" data-testid="model-group-rollout-progress-Rig" aria-label="Proto Rig: ${status}. Updating to ${version}" aria-valuemin="0" aria-valuemax="100" aria-valuenow="50">50%</span>`;

async function renderCreateChannel(page: Page, listContent = '<div data-testid="channels-table">Channels</div>') {
  await page.setContent(`
    ${responsiveModalActions}
    <div id="channels">
      <button onclick="document.querySelector('#modals').innerHTML = document.querySelector('#create').innerHTML">Create release channel</button>
      ${listContent}
    </div>
    <div id="modals"></div>
    <div data-testid="toaster-container"></div>
    <template id="create"><section data-testid="create-release-channel-modal">
      <h2>Create release channel</h2>
      <button aria-label="Close dialog" onclick="this.closest('section').remove()">Close</button>
      <section data-testid="release-channel-new">
        <input id="channel-name" />
        <button data-testid="rollout-method" onclick="document.querySelector('#methods').hidden = false">Single batch</button>
        <div id="methods" hidden>
          <button role="option" onclick="document.querySelector('[data-testid=rollout-method]').textContent = this.textContent; this.parentElement.hidden = true">Pilot batch, then remaining</button>
        </div>
        <input id="pilot-size" type="number" value="1" />
        <p data-testid="scope-preview">This channel covers 2 miners</p>
        ${modalSaveButtons("Create channel", `document.querySelector('[data-testid=toaster-container]').textContent = 'Created release channel ${channel}'; document.querySelector('#channels').innerHTML = '<section data-testid=&quot;release-channel-${channel}&quot;>Channel created</section>'; document.querySelector('#modals').replaceChildren()`)}
      </section>
    </section></template>
  `);
}

test.describe("Firmware rollout helper guards", { tag: "@smoke" }, () => {
  test("new-channel helpers edit in the create modal and wait for its dismissal after saving", async ({
    page,
    isMobile,
  }) => {
    await renderCreateChannel(page);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.startCreateChannel(channel);
    const modal = page.getByTestId("create-release-channel-modal");
    await expect(modal.locator("#channel-name")).toHaveValue(channel);
    await expect(page.getByTestId("channels-table")).toBeVisible();
    await firmware.setMethod("Pilot batch, then remaining");
    await firmware.setPilotSize(2);
    await firmware.validateScopeCovers(2);
    await expect(modal.getByTestId("rollout-method")).toHaveText("Pilot batch, then remaining");
    await expect(modal.locator("#pilot-size")).toHaveValue("2");
    await expect(page.getByTestId("channel-settings-modal")).toHaveCount(0);
    await expect(modal.getByTestId(isMobile ? "save-channel" : "save-channel-mobile")).toBeHidden();
    await expect(modal.getByTestId(isMobile ? "save-channel-mobile" : "save-channel")).toBeVisible();
    await firmware.saveNewChannel(channel);
    await expect(modal).toBeHidden();
    await expect(firmware.channelView(channel)).toBeVisible();
  });

  for (const hasChannels of [true, false]) {
    test(`returning to ${hasChannels ? "the channels table" : "the empty state"} dismisses unsaved creation`, async ({
      page,
    }) => {
      await renderCreateChannel(
        page,
        hasChannels ? '<div data-testid="channels-table">Channels</div>' : "<h2>No release channels</h2>",
      );
      const firmware = new SettingsFirmwarePage(page);
      await firmware.startCreateChannel(channel);
      await expect(page.getByTestId("back-to-channels")).toHaveCount(0);
      await firmware.backToChannels();
      await expect(page.getByTestId("create-release-channel-modal")).toBeHidden();
      await expect(page.getByRole("button", { name: "Create release channel", exact: true })).toBeVisible();
      await expect(firmware.channelView(channel)).toHaveCount(0);
    });
  }

  test("existing-channel settings helpers review and apply before viewing history", async ({ page, isMobile }) => {
    await renderTables(page, completed);
    await page.locator("body").evaluate(
      (body, content) => body.insertAdjacentHTML("beforeend", content),
      `<button data-testid="channel-settings" onclick="document.querySelector('[data-testid=channel-settings-modal]').hidden = false">Channel settings</button>
      <section data-testid="channel-settings-modal" hidden style="position: fixed; inset: 0; background: white">
        <button aria-label="Close dialog" onclick="this.closest('section').hidden = true">Close</button>
        <input id="pilot-size" type="number" value="1" />
        <p data-testid="scope-preview">This channel covers 2 miners</p>
        ${modalSaveButtons("Review changes", "document.querySelector('[data-testid=channel-settings-modal]').hidden = true; document.querySelector('[data-testid=apply-firmware-dialog]').hidden = false")}
      </section>
      <section data-testid="apply-firmware-dialog" hidden>
        <h2>Apply channel changes?</h2>
        <p>Channel settings: Pilot size 1 → 2</p>
        <button onclick="document.querySelector('[data-testid=toaster-container]').textContent = 'Channel changes applied'; this.closest('section').hidden = true">Apply changes</button>
      </section>
      <button onclick="document.querySelector('[data-testid=toaster-container]').textContent = 'Wrong Apply action'">Apply changes</button>
      <div data-testid="toaster-container"></div>`,
    );
    const firmware = new SettingsFirmwarePage(page);
    await firmware.setPilotSize(2);
    await expect(page.getByTestId(isMobile ? "save-channel" : "save-channel-mobile")).toBeHidden();
    await expect(page.getByTestId(isMobile ? "save-channel-mobile" : "save-channel")).toBeVisible();
    await firmware.saveChannelChanges();
    await expect(page.getByTestId("channel-settings-modal")).toBeHidden();
    await expect(page.getByTestId("apply-firmware-dialog")).toBeHidden();

    await firmware.validateHistoryOutcome(channel, version, "Completed");
    await expect(page.getByTestId("channel-settings-modal")).toBeHidden();
    await firmware.validateScopeCovers(2);
    await expect(page.getByTestId("channel-settings-modal")).toBeVisible();
    await expect(page.locator("#pilot-size")).toHaveValue("2");
  });

  test("reviewing settings with staged firmware leaves both changes pending until confirmation", async ({ page }) => {
    await page.setContent(`
      ${responsiveModalActions}
      <button data-testid="channel-settings" onclick="document.querySelector('[data-testid=channel-settings-modal]').hidden = false">Channel settings</button>
      <button data-testid="apply-firmware-changes" aria-label="Apply changes (1)" onclick="document.querySelector('[data-testid=apply-firmware-dialog]').hidden = false">
        Apply changes <span data-testid="pending-change-count" aria-hidden="true">1</span>
      </button>
      <section data-testid="channel-settings-modal" hidden>
        <input id="pilot-size" type="number" value="1" oninput="document.querySelector('[data-testid=pending-change-count]').textContent = '2'; document.querySelector('[data-testid=apply-firmware-changes]').setAttribute('aria-label', 'Apply changes (2)')" />
        ${modalSaveButtons("Review changes", "document.querySelector('[data-testid=channel-settings-modal]').hidden = true; document.querySelector('[data-testid=apply-firmware-dialog]').hidden = false")}
      </section>
      <section data-testid="apply-firmware-dialog" hidden>
        <h2>Apply channel changes?</h2>
        <p role="status">2 changes pending</p>
        <p>Channel settings: Pilot size 1 → 2</p>
        <table aria-label="Firmware changes">
          <thead><tr><th>Model</th><th>Original</th><th>Target</th></tr></thead>
          <tbody><tr><th scope="row">Proto Rig</th><td>3.1.123</td><td>${version}</td></tr></tbody>
        </table>
        <button onclick="document.querySelector('[data-testid=toaster-container]').textContent = 'Channel changes applied'; this.closest('section').hidden = true">Apply changes</button>
      </section>
      <div data-testid="toaster-container"></div>
    `);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.setPilotSize(2);
    const apply = page.getByRole("button", { name: "Apply changes (2)", exact: true });
    await expect(apply.getByTestId("pending-change-count")).toHaveText("2");
    await expect(page.getByText("2 changes pending")).toBeHidden();
    await firmware.reviewChannelChanges();
    const preview = page.getByTestId("apply-firmware-dialog");
    await expect(preview.getByText("2 changes pending")).toBeVisible();
    await expect(preview).toContainText("Channel settings: Pilot size 1 → 2");
    const row = preview.getByRole("table", { name: "Firmware changes" }).getByRole("row", { name: /Proto Rig/ });
    await expect(row.getByRole("cell")).toHaveText(["3.1.123", version]);
    await expect(apply.getByTestId("pending-change-count")).toBeVisible();
    await expect(page.getByTestId("toaster-container")).toBeEmpty();
  });

  for (const creating of [true, false]) {
    const modalTestId = creating ? "create-release-channel-modal" : "channel-settings-modal";
    const action = creating ? "Create channel" : "Review changes";

    test(`scope conflicts check the visible ${creating ? "creation" : "settings"} action`, async ({ page }) => {
      await page.setContent(`
        ${responsiveModalActions}
        <section data-testid="${modalTestId}">
          <p data-testid="scope-conflicts">Overlaps another channel</p>
          ${modalSaveButtons(action)}
        </section>
      `);
      // Only the visible action is disabled, so checking its hidden duplicate
      // cannot accidentally satisfy the helper's conflict assertion.
      await page.getByRole("button", { name: action, exact: true }).evaluate((button) => {
        button.setAttribute("disabled", "");
      });
      await new SettingsFirmwarePage(page).validateScopeConflict("another channel");
    });
  }

  test("history does not accept the requested version in another row's rollback action", async ({ page }) => {
    await renderTables(
      page,
      `<tr data-testid="history-row-3"><td>Completed</td><td>Proto Rig</td><td>3.1.123</td><td><button>Roll back to ${version}</button></td></tr>
       <tr data-testid="history-row-2"><td>Failed</td><td>Proto Rig</td><td>${version}</td><td></td></tr>`,
    );
    await expect(
      new SettingsFirmwarePage(page).validateHistoryOutcome(channel, version, "Completed", 300),
    ).rejects.toThrow(/Failed/);
  });

  test("history does not accept an older completed rollout or a completed-with-failures outcome", async ({ page }) => {
    await renderTables(
      page,
      `<tr data-testid="history-row-3"><td>Completed with failures</td><td>Proto Rig</td><td>${version}</td><td></td></tr>${completed}`,
    );
    await expect(
      new SettingsFirmwarePage(page).validateHistoryOutcome(channel, version, "Completed", 300),
    ).rejects.toThrow(/Completed with failures/);
  });

  for (const count of [0, 1]) {
    test(`completion rejects ${count} rows when two miners are expected`, async ({ page }) => {
      await renderTables(page, completed, count === 1 ? miner(1, version) : "");
      await expect(
        new SettingsFirmwarePage(page).waitForChannelUpdateCompleted(channel, target, version, 2, 300),
      ).rejects.toThrow(/toHaveCount/);
    });
  }

  test("active update and review helpers read the progress bar's accessible status", async ({ page }) => {
    await renderTables(page, "", "", rolloutProgress());
    const firmware = new SettingsFirmwarePage(page);
    await firmware.validateChannelUpdateInProgress(channel, version);

    await renderTables(page, "", "", rolloutProgress("Review needed"));
    await firmware.waitForModelReviewNeeded(channel, 300);
  });

  test("completion rejects an ongoing progress bar even after miners report the target version", async ({ page }) => {
    await renderTables(page, completed, miner(1, version) + miner(2, version), rolloutProgress());
    await expect(
      new SettingsFirmwarePage(page).waitForChannelUpdateCompleted(channel, target, version, 2, 300),
    ).rejects.toThrow(/toBeHidden/);
  });

  test("completion requires the exact reported firmware, not the miner name or a version prefix", async ({ page }) => {
    await renderTables(page, completed, miner(1, `${version}-old`) + miner(2, version));
    await expect(
      new SettingsFirmwarePage(page).waitForChannelUpdateCompleted(channel, target, version, 2, 300),
    ).rejects.toThrow(/toHaveText/);
  });

  test("completion accepts the expected miners and exact completed history entry", async ({ page }) => {
    await renderTables(page, completed, miner(1, version) + miner(2, version));
    await new SettingsFirmwarePage(page).waitForChannelUpdateCompleted(channel, target, version, 2, 300);
    await expect(page.getByTestId("modal").filter({ visible: true })).toHaveCount(0);
  });

  test("miner identity remains distinct when display names are identical", async ({ page }) => {
    await renderTables(
      page,
      completed,
      `<tr data-testid="channel-miner-one"><td>Proto Rig</td><td>${version}</td></tr>
       <tr data-testid="channel-miner-two"><td>Proto Rig</td><td>${version}</td></tr>`,
    );
    expect(await new SettingsFirmwarePage(page).getChannelMinerIdentifiers(channel, target)).toEqual([
      "channel-miner-one",
      "channel-miner-two",
    ]);
  });

  test("firmware cleanup removes suite leftovers and preserves unrelated filename cells", async ({ page }) => {
    const names = ["e2e-firmware-rollout-leftover.swu", "manual.swu", "archive-e2e-firmware-rollout-leftover.swu"];
    await page.setContent(`
      <button>Upload firmware</button>
      <table><tbody data-testid="list-body">
        ${names
          .map(
            (name, index) => `<tr id="file-${index}" data-testid="list-row">
          <td data-testid="filename">${name}</td><td>e2e-firmware-rollout-leftover.swu</td>
          <td><button aria-label="Row actions" onclick="const menu = document.querySelector('#row-menu'); menu.hidden = false; menu.dataset.target = 'file-${index}'">Actions</button></td>
        </tr>`,
          )
          .join("")}
      </tbody></table>
      <section id="row-menu" hidden>
        <button onclick="const menu = this.closest('section'); const dialog = document.querySelector('#delete-dialog'); dialog.dataset.target = menu.dataset.target; dialog.hidden = false; menu.hidden = true">Delete</button>
      </section>
      <section id="delete-dialog" data-testid="delete-firmware-dialog" hidden>
        <button onclick="const dialog = this.closest('section'); document.getElementById(dialog.dataset.target).remove(); dialog.hidden = true">Delete</button>
      </section>
    `);
    await new SettingsFirmwarePage(page).deleteFirmwareFilesWithPrefix("e2e-firmware-rollout-");
    await expect(page.getByTestId("filename")).toHaveText(names.slice(1));
  });

  test("firmware cleanup rejects an empty ownership prefix", async ({ page }) => {
    await expect(new SettingsFirmwarePage(page).deleteFirmwareFilesWithPrefix("")).rejects.toThrow(
      /nonempty suite prefix/,
    );
  });

  test("a failed catalog request cannot be mistaken for empty firmware cleanup", async ({ page }) => {
    await page.route("http://firmware.test/api/v1/firmware/files", (route) =>
      route.fulfill({ status: 500, contentType: "application/json", body: '{"error":"Catalog unavailable"}' }),
    );
    await page.route("http://firmware.test/catalog", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: `<button onclick="fetch('/api/v1/firmware/files')">Files</button><button>Upload firmware</button><p>No firmware files uploaded</p>`,
      }),
    );
    await page.goto("http://firmware.test/catalog");
    await expect(new SettingsFirmwarePage(page).openFilesTab()).rejects.toThrow(
      /Firmware catalog request failed \(500\)/,
    );
  });

  test("channel cleanup accepts the loaded empty state", async ({ page }) => {
    await page.setContent("<button>Create release channel</button><h2>No release channels</h2>");
    await new SettingsFirmwarePage(page).deleteChannelIfPresent("E2E firmware rollout A");
    await expect(page.getByRole("heading", { name: "No release channels" })).toBeVisible();
  });
});
