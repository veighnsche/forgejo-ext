// @watch start
// templates/**
// web_src/js/**
// contrib/extensions/example/**
// routers/web/extensions/**
// @watch end

import {expect} from '@playwright/test';
import {test} from './utils_e2e.ts';

test.use({user: 'user2'});

test('example keeps native navigation, panel lifetime and session authority', async ({page, browser}) => {
  await page.goto('/user2/repo1/workspace');
  const notes = page.locator('#example-persistent-notes');
  await expect(notes).toBeEditable();
  await notes.fill('Fountain browser draft');
  await notes.evaluate((element) => { element.dataset.fountainProbe = 'original-mount' });
  await page.locator('overflow-menu a[href="/user2/repo1/issues"]').click();
  await expect(page).toHaveURL(/\/user2\/repo1\/issues$/);
  await expect(notes).toHaveValue('Fountain browser draft');
  await expect(notes).toHaveAttribute('data-fountain-probe', 'original-mount');

  await page.locator('overflow-menu a[href="/user2/repo1"]').click();
  const branchButton = page.locator('.branch-dropdown-button').first();
  await branchButton.click();
  await expect(page.locator('.js-branch-tag-selector .menu.visible')).toBeVisible();
  await branchButton.click();
  await expect(notes).toHaveAttribute('data-fountain-probe', 'original-mount');
  await expect(page.locator('[data-panel-lifetime]')).toHaveAttribute('title', /user2\/repo1/);

  const denied = await page.request.get('/admin/extensions');
  expect(denied.status()).toBe(403);
  const guest = await browser.newContext();
  try {
    const response = await guest.request.get('/-/native/panels/example/notes/api/notes');
    expect(response.status()).toBe(401);
  } finally {
    await guest.close();
  }

  const admin = await browser.newContext({storageState: 'tests/e2e/.auth/state-chromium-user1-shared.json'});
  try {
    const settings = await admin.newPage();
    await settings.goto('/admin/extensions');
    const row = settings.locator('#ext-row-example');
    await expect(row).toHaveAttribute('data-ext-state', 'enabled');
    await row.getByRole('button', {name: 'Disable', exact: true}).click();
    await expect(row).toHaveAttribute('data-ext-state', 'disabled', {timeout: 10000});
    const disabled = await page.request.get('/-/native/panels/example/notes/api/notes');
    expect(disabled.ok()).toBe(false);
    await row.getByRole('button', {name: 'Enable', exact: true}).click();
    await expect(row).toHaveAttribute('data-ext-state', 'enabled', {timeout: 10000});
  } finally {
    await admin.close();
  }

  await page.goto('/workspace');
  await expect(page.locator('[data-extension-context]')).toContainText('"actor"');
  const logout = await page.request.post('/user/logout');
  expect(logout.ok()).toBe(true);
  const revoked = await page.request.get('/-/native/panels/example/notes/api/notes');
  expect(revoked.status()).toBe(401);
  await page.reload();
  await expect(page.locator('#example-persistent-notes')).toHaveCount(0);
});
