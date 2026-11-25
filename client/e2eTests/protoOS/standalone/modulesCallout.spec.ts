import { expect, type Locator, test } from "@playwright/test";

const horizontalEdges = (locator: Locator) =>
  locator.evaluate((element) => {
    const { left, right } = element.getBoundingClientRect();
    return { left, right };
  });

test("the no-pools banner stays aligned with the Modules heading when the table scrolls", async ({ page }) => {
  // An empty ListPools protobuf response represents a CT1 with no configured pools.
  await page.route("**/mdk.v2.PoolService/ListPools", (route) =>
    route.fulfill({ contentType: "application/proto", body: Buffer.alloc(0) }),
  );
  await page.goto("/modules");
  const heading = page.getByRole("heading", { name: "Modules", level: 1, exact: true });
  const banner = page.getByTestId("callout").filter({ hasText: "No mining pools configured." });
  const scrollport = page.getByTestId("list-scrollport");
  await expect(banner).toBeVisible();
  await expect(page.getByRole("table")).toBeVisible();

  const expected = await horizontalEdges(heading);
  const viewport = await horizontalEdges(scrollport);
  expect(expected.left).toBeGreaterThan(viewport.left);
  expect(expected.right).toBeLessThan(viewport.right);
  expect(await horizontalEdges(banner)).toEqual(expected);

  const distance = await scrollport.evaluate((element) => {
    element.scrollLeft = element.scrollWidth;
    return element.scrollLeft;
  });
  expect(distance).toBeGreaterThan(0);
  expect(await horizontalEdges(heading)).toEqual(expected);
  expect(await horizontalEdges(banner)).toEqual(expected);
});
