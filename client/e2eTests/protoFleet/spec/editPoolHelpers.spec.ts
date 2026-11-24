import { expect, test } from "@playwright/test";
import { EditPoolPage } from "../pages/editPool";

test.use({ storageState: { cookies: [], origins: [] } });

test("configuration selection waits for the saved configurations to load", async ({ page, isMobile }) => {
  await page.setContent(`
    <div role="dialog" aria-label="Edit pool">
      <p id="loading">Loading configurations</p>
      <div id="configurations"></div>
    </div>
    <script>
      setTimeout(() => {
        document.getElementById('loading').remove();
        document.getElementById('configurations').innerHTML =
          '<label><input type="radio" name="configuration">Production</label>';
      }, 150);
    </script>
  `);
  const picker = new EditPoolPage(page, isMobile);
  await picker.selectConfiguration("Production");
  await picker.validateSelectedConfiguration("Production");
  await expect(page.getByText("Loading configurations")).toBeHidden();
});
