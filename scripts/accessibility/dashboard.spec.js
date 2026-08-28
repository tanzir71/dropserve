const { test, expect } = require('@playwright/test');
const AxeBuilder = require('@axe-core/playwright').default;

const wcagAATags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

async function openReadyDashboard(page) {
  await page.goto('/');
  await expect(page.locator('body')).toContainText('Dropserve');
  await expect(page.locator('#app-grid')).toHaveAttribute('aria-busy', 'false');
  await expect(page.locator('.app-card')).toHaveCount(1);
}

for (const colorScheme of ['light', 'dark']) {
  test.describe(`${colorScheme} theme`, () => {
    test.use({ colorScheme });

    test('has named controls and no automated WCAG A or AA violations', async ({ page }) => {
      await openReadyDashboard(page);

      const controls = page.locator('a:visible, button:visible, input:visible, select:visible, textarea:visible, [role="button"]:visible');
      for (let index = 0; index < await controls.count(); index += 1) {
        await expect(controls.nth(index)).toHaveAccessibleName(/\S/);
      }
      await expect(page.locator('#app-search')).toHaveAccessibleName('Search your apps');

      const results = await new AxeBuilder({ page }).withTags(wcagAATags).analyze();
      expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
    });
  });
}

test('Tab reaches the app and every opened action', async ({ page }) => {
  await openReadyDashboard(page);

  const search = page.locator('#app-search');
  const appLink = page.locator('.app-card > a');
  const actionsToggle = page.locator('.card-actions-toggle');
  await search.focus();
  await page.keyboard.press('Tab');
  await expect(appLink).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(actionsToggle).toBeFocused();
  await page.keyboard.press('Enter');
  await page.keyboard.press('Tab');
  await expect(page.locator('.card-actions button').first()).toBeFocused();

  const sharingToggle = page.locator('#sharing-toggle');
  await sharingToggle.focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('#sharing-close')).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.locator('#sharing-panel a:visible, #sharing-panel button:visible').nth(1)).toBeFocused();
});

test('mobile card actions survive overlap and a live rerender', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openReadyDashboard(page);

  await page.evaluate(() => {
    const grid = document.querySelector('#app-grid');
    const card = grid.querySelector('.app-card');
    grid.append(card.cloneNode(true));
  });

  const firstCard = page.locator('.app-card').first();
  await firstCard.locator('.card-actions-toggle').click();
  const finalAction = firstCard.getByRole('button', { name: 'Hide from index' });
  await expect(finalAction).toBeVisible();
  await finalAction.click({ trial: true });

  await page.locator('#app-search').fill('field');
  await expect(finalAction).toBeVisible();
  await finalAction.click({ trial: true });
});
