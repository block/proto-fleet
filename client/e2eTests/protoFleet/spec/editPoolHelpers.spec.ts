import { expect, test } from "@playwright/test";
import { EditPoolPage } from "../pages/editPool";

test.use({ storageState: { cookies: [], origins: [] } });

test("removing pools waits for existing assignments to load", async ({ page, isMobile }) => {
  await page.setContent(`
    <p id="loading">Loading pool configuration...</p>
    <div id="editor"></div>
    <script>
      setTimeout(() => {
        document.getElementById('loading').remove();
        document.getElementById('editor').innerHTML = \`
          <div data-testid="pool-row-0">
            <button aria-label="Pool actions" onclick="document.getElementById('remove').hidden = false">Actions</button>
          </div>
          <button id="remove" hidden onclick="document.querySelector('[data-testid=pool-row-0]').remove(); this.remove()">Remove</button>
        \`;
      }, 150);
    </script>
  `);

  await new EditPoolPage(page, isMobile).removeAllPools();

  await expect(page.getByText("Loading pool configuration...", { exact: true })).toBeHidden();
  await expect(page.getByTestId(/^pool-row-\d+$/)).toHaveCount(0, { timeout: 500 });
});
