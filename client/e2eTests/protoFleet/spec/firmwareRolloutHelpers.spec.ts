import { expect, type Page, test } from "@playwright/test";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";

const channel = "Helper fixture";
const target = { manufacturer: "Proto", model: "Rig" };
const version = "3.2.123";

// Exercise the real page-object locators against the table structure used by
// the UI, including misleading text in other cells. No backend state is used.
test.use({ storageState: { cookies: [], origins: [] } });

async function renderTables(page: Page, historyRows: string, minerRows = "") {
  await page.setContent(`
    <section data-testid="release-channel-${channel}">
      <button data-testid="channel-history" onclick="document.querySelector('#modals').innerHTML = document.querySelector('#history').innerHTML">History</button>
      <table><tbody><tr data-testid="model-group-Rig">
        <td>Proto Rig</td>
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

test.describe("Firmware rollout helper guards", { tag: "@smoke" }, () => {
  test("new-channel settings helpers keep using the inline form", async ({ page }) => {
    await page.setContent(`
      <section data-testid="release-channel-new">
        <button data-testid="rollout-method" onclick="document.querySelector('#methods').hidden = false">Single batch</button>
        <div id="methods" hidden>
          <button role="option" onclick="document.querySelector('[data-testid=rollout-method]').textContent = this.textContent; this.parentElement.hidden = true">Pilot batch, then remaining</button>
        </div>
        <input id="pilot-size" type="number" value="1" />
        <p data-testid="scope-preview">This channel covers 2 miners</p>
      </section>
    `);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.setMethod("Pilot batch, then remaining");
    await firmware.setPilotSize(2);
    await firmware.validateScopeCovers(2);
    await expect(page.getByTestId("rollout-method")).toHaveText("Pilot batch, then remaining");
    await expect(page.locator("#pilot-size")).toHaveValue("2");
    await expect(page.getByTestId("channel-settings-modal")).toHaveCount(0);
  });

  test("existing-channel settings helpers open the modal and dismiss it before viewing history", async ({ page }) => {
    await renderTables(page, completed);
    await page.locator("body").evaluate((body) => {
      body.insertAdjacentHTML(
        "beforeend",
        `<button data-testid="channel-settings" onclick="document.querySelector('[data-testid=channel-settings-modal]').hidden = false">Channel settings</button>
        <section data-testid="channel-settings-modal" hidden style="position: fixed; inset: 0; background: white">
          <button aria-label="Close dialog" onclick="this.closest('section').hidden = true">Close</button>
          <input id="pilot-size" type="number" value="1" />
          <p data-testid="scope-preview">This channel covers 2 miners</p>
          <button data-testid="save-channel" onclick="document.querySelector('[data-testid=toaster-container]').textContent = 'Release channel saved'">Save changes</button>
        </section>
        <div data-testid="toaster-container"></div>`,
      );
    });
    const firmware = new SettingsFirmwarePage(page);
    await firmware.setPilotSize(2);
    await firmware.saveChannelChanges();
    await expect(page.getByTestId("channel-settings-modal")).toBeVisible();

    await firmware.validateHistoryOutcome(channel, version, "Completed");
    await expect(page.getByTestId("channel-settings-modal")).toBeHidden();
    await firmware.validateScopeCovers(2);
    await expect(page.getByTestId("channel-settings-modal")).toBeVisible();
    await expect(page.locator("#pilot-size")).toHaveValue("2");
  });

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
