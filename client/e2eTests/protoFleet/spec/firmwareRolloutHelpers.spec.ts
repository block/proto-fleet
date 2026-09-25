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
  test("modal title helpers ignore the retained hidden title when it collapses into the header", async ({ page }) => {
    await page.setContent(`
      <section data-testid="modal">
        <div id="header-title" class="text-heading-200" style="visibility:hidden">Channel settings</div>
        <div id="body-title" class="text-heading-300">Channel settings</div>
      </section>
    `);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.validateTitleInModal("Channel settings");
    await page.evaluate(() => {
      document.querySelector<HTMLElement>("#header-title")!.style.visibility = "visible";
      document.querySelector<HTMLElement>("#body-title")!.style.visibility = "hidden";
    });
    await firmware.validateTitleInModal("Channel settings");
    await page.getByTestId("modal").evaluate((modal) => modal.setAttribute("hidden", ""));
    await firmware.validateTitleInModalNotVisible("Channel settings");
  });

  for (const navigation of ["tab", "header pill"]) {
    const openChannels = navigation === "tab" ? "openReleaseChannelsTab" : "followAppRolloutPillToChannels";
    for (const hasChannels of [true, false]) {
      test(`${navigation} navigation waits for the selected tab and loaded ${hasChannels ? "channels table" : "empty state"}`, async ({
        page,
      }) => {
        await page.setContent(`
          ${responsiveModalActions}
          <h1 class="text-heading-400">Firmware</h1>
          <button onclick="document.querySelector('#unexpected').textContent = 'Wrong tab'">Release channels</button>
          <button aria-label="View ongoing firmware updates" onclick="document.querySelector('#pill-popover').hidden = false">Firmware updates</button>
          <div id="pill-popover" hidden><a href="#release-channels" onclick="openChannels(); this.parentElement.hidden = true">View release channels</a></div>
          <div data-testid="firmware-tab-navigation">
            <div><button aria-current="page"><span>Files</span></button></div>
            <div><button id="channels-tab" onclick="openChannels()"><span>Release channels</span></button></div>
          </div>
          <div id="channel-loading" hidden>Loading release channels...</div>
          <div id="channel-content" hidden>
            <button>Create release channel</button>
            ${hasChannels ? '<div data-testid="channels-table">Channels</div>' : "<h2>No release channels</h2>"}
          </div>
          <button id="finish-loading" onclick="document.querySelector('#channel-loading').hidden = true; document.querySelector('#channel-content').hidden = false">Finish fixture loading</button>
          <p id="unexpected"></p>
          <script>
            function openChannels() {
              document.querySelector('[aria-current]').removeAttribute('aria-current');
              document.querySelector('#channels-tab').setAttribute('aria-current', 'page');
              document.querySelector('#channel-loading').hidden = false;
            }
          </script>
        `);
        const firmware = new SettingsFirmwarePage(page);
        const opened = firmware[openChannels]();
        await expect(page.getByText("Loading release channels...", { exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Create release channel", exact: true })).toBeHidden();
        await page.locator("#finish-loading").click();
        await opened;
        await expect(
          page.getByTestId("firmware-tab-navigation").getByRole("button", { name: "Release channels" }),
        ).toHaveAttribute("aria-current", "page");
        await expect(page.getByRole("button", { name: "Create release channel", exact: true })).toBeVisible();
        await expect(page.locator("#unexpected")).toBeEmpty();
      });
    }
  }

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
    await firmware.setPilotSize(3);
    await expect(page.getByTestId("channel-settings-modal")).toBeVisible();
    await expect(page.locator("#pilot-size")).toHaveValue("3");
  });

  test("scope removal checks the draft selection and its Original and Target preview before applying", async ({
    page,
  }) => {
    await page.setContent(`
      ${responsiveModalActions}
      <button>Miners 2 miners</button>
      <section data-testid="channel-settings-modal">
        <div data-testid="scope-editor"><button>Miners 1 miner</button></div>
        ${modalSaveButtons("Review changes", "document.querySelector('[data-testid=channel-settings-modal]').hidden = true; document.querySelector('[data-testid=apply-firmware-dialog]').hidden = false")}
      </section>
      <section data-testid="apply-firmware-dialog" hidden>
        <table aria-label="Channel settings changes">
          <thead><tr><th>Setting</th><th>Original</th><th>Target</th></tr></thead>
          <tbody>
            <tr><th scope="row">Name</th><td>Old channel</td><td>Rig A (miner-a)</td></tr>
            <tr><th scope="row">Applies to · Miners</th><td>Rig A (miner-a)\nRig B (miner-b)</td><td>Rig B (miner-b)</td></tr>
          </tbody>
        </table>
        <button onclick="document.querySelector('[data-testid=toaster-container]').textContent = 'Channel changes applied'; this.closest('section').hidden = true">Apply changes</button>
      </section>
      <div data-testid="toaster-container"></div>
    `);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.validateScopeMinerSelection(1);
    await firmware.reviewChannelChanges();
    await firmware.validateScopeMinerRemoval("Rig A", "Rig B");
    await expect(page.getByTestId("toaster-container")).toBeEmpty();
    await firmware.confirmChannelChanges();
    await expect(page.getByTestId("apply-firmware-dialog")).toBeHidden();
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

  test("scope conflicts check the visible creation action", async ({ page }) => {
    await page.setContent(`
        ${responsiveModalActions}
        <section data-testid="create-release-channel-modal">
          <p data-testid="scope-conflicts">Overlaps another channel</p>
          ${modalSaveButtons("Create channel")}
        </section>
      `);
    // Only the visible action is disabled, so checking its hidden duplicate
    // cannot accidentally satisfy the helper's conflict assertion.
    await page.getByRole("button", { name: "Create channel", exact: true }).evaluate((button) => {
      button.setAttribute("disabled", "");
    });
    await new SettingsFirmwarePage(page).validateScopeConflict("another channel");
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

  test("active update and review helpers read the progress bar's accessible status", async ({ page }) => {
    await renderTables(page, "", "", rolloutProgress());
    const firmware = new SettingsFirmwarePage(page);
    await firmware.validateChannelUpdateInProgress(channel, version);

    await renderTables(page, "", "", rolloutProgress("Review needed"));
    await firmware.waitForModelReviewNeeded(channel, 300);
  });

  for (const presentation of ["inline", "View update", "Review update"]) {
    test(`active update helper opens the exact channel and model through ${presentation}`, async ({ page }) => {
      const title = `${channel}, Proto Rig firmware update`;
      const wrongAction = "document.querySelector('#unexpected').textContent = 'Wrong update'";
      const openDetail = "document.querySelector('[data-testid=modal]').hidden = false";
      const distractors = [
        `${channel} archive, Proto Rig firmware update`,
        `${channel}, Other Rig firmware update`,
        `${channel}, Proto Rig Pro firmware update`,
        `Other channel, Proto Rig firmware update`,
      ];
      await page.setContent(`
        ${responsiveModalActions}
        ${distractors.map((name, index) => `<section data-testid="active-update-${index}"><p>${name}</p><button onclick="${wrongAction}">View update</button></section>`).join("")}
        <section data-testid="active-update-42">
          <span>${title}</span>
          ${
            presentation === "inline"
              ? `<section data-testid="inline-rollout-live-view">
                <button data-testid="inline-view-rollout-more-actions-trigger" onclick="document.querySelector('[data-testid=inline-view-rollout-more-actions-menu]').hidden = false">More actions</button>
              </section>`
              : `<button onclick="${openDetail}">${presentation}</button>`
          }
        </section>
        <button data-testid="inline-view-rollout-more-actions-trigger" onclick="${wrongAction}">Unrelated actions</button>
        <button data-testid="inline-view-rollout-open-action" onclick="${wrongAction}">View update</button>
        <section data-testid="inline-view-rollout-more-actions-menu" hidden>
          <button data-testid="inline-view-rollout-open-action" onclick="${openDetail}; this.closest('section').hidden = true">View update</button>
        </section>
        <section data-testid="modal" hidden>
          <h2 class="heading">${title}</h2>
          <p>Update status</p>
        </section>
        <p id="unexpected"></p>
      `);
      await new SettingsFirmwarePage(page).openActiveUpdate(channel, target);
      await expect(page.getByTestId("modal")).toBeVisible();
      await expect(page.locator("#unexpected")).toBeEmpty();
    });
  }

  test("detail evidence helper reads verification and telemetry together, apart from plan metadata", async ({
    page,
  }) => {
    await page.setContent(`
      <section data-testid="rollout-evidence"><p>Another update's evidence</p></section>
      <section data-testid="modal">
        <section data-testid="rollout-detail-stats"><p>Scope</p><p>Method</p></section>
        <section data-testid="rollout-evidence">
          <div data-testid="evidence-online">Back online: 2 of 2</div>
          <div data-testid="evidence-hashing">Hashing: 2 of 2</div>
          <div data-testid="evidence-hashrate">Hashrate: 230 TH/s</div>
          <div data-testid="evidence-errors">New errors: 0</div>
        </section>
      </section>
    `);
    await new SettingsFirmwarePage(page).validateEvidenceVisible();
  });

  test("detail miners open directly from the live progress without opening other actions", async ({ page }) => {
    await page.setContent(`
      ${responsiveModalActions}
      <button data-testid="view-rollout-view-miners-action">Another update's miners</button>
      <section data-testid="modal">
        <button data-testid="view-rollout-more-actions-trigger" onclick="document.body.dataset.openedMenu = 'true'">More actions</button>
        <section aria-label="Update progress">
          <button data-testid="view-rollout-view-miners-action" onclick="document.querySelector('[data-testid=rollout-miners-modal]').hidden = false">View miners</button>
        </section>
      </section>
      <section data-testid="rollout-miners-modal" hidden>
        <table><tbody>
          <tr data-testid="list-row"><td>Miner one</td></tr>
          <tr data-testid="list-row"><td>Miner two</td></tr>
        </tbody></table>
        <button onclick="this.closest('section').hidden = true">Done</button>
      </section>
    `);
    await new SettingsFirmwarePage(page).validateDetailMinersCount(2);
    await expect(page.getByTestId("rollout-miners-modal")).toBeHidden();
    await expect(page.getByTestId("modal")).toBeVisible();
    await expect(page.locator("body")).not.toHaveAttribute("data-opened-menu");
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

  test("channel deletion opens the intended settings and confirms only its delete dialog", async ({ page }) => {
    await page.setContent(`
      ${responsiveModalActions}
      <section data-testid="release-channel-Unrelated">
        <button data-testid="channel-settings" onclick="document.querySelector('#unexpected').textContent = 'Wrong channel'">Channel settings</button>
        <button data-testid="delete-channel" onclick="document.querySelector('#unexpected').textContent = 'Wrong delete action'">Delete channel</button>
      </section>
      <section data-testid="release-channel-${channel}">
        <button data-testid="channel-settings" onclick="document.querySelector('[data-testid=channel-settings-modal]').hidden = false">Channel settings</button>
      </section>
      <section data-testid="channel-settings-modal" hidden>
        <h2>Channel settings</h2>
        <h3>Delete this channel</h3>
        <button data-testid="delete-channel" onclick="document.querySelector('[data-testid=delete-channel-dialog]').hidden = false">Delete channel</button>
      </section>
      <section data-testid="delete-channel-dialog" hidden>
        <h2>Delete release channel?</h2>
        <p>Miners in ${channel} keep their current firmware, but it is no longer enforced for them and the channel's update history is removed.</p>
        <button onclick="document.querySelector('[data-testid=channel-settings-modal]').remove(); document.querySelector('#target-row').remove(); this.closest('section').remove(); document.querySelector('#deleted').textContent = '${channel}'">Delete channel</button>
      </section>
      <table><tbody>
        <tr id="target-row" data-testid="list-row"><td data-testid="channel-row-${channel}">${channel}</td></tr>
        <tr data-testid="list-row"><td data-testid="channel-row-Unrelated">Unrelated</td></tr>
      </tbody></table>
      <p id="unexpected"></p>
      <p id="deleted"></p>
    `);
    const firmware = new SettingsFirmwarePage(page);
    await firmware.deleteChannel(channel);
    await expect(page.locator("#deleted")).toHaveText(channel);
    await expect(page.locator("#unexpected")).toBeEmpty();
    await expect(firmware.channelRow("Unrelated")).toBeVisible();
  });
});
