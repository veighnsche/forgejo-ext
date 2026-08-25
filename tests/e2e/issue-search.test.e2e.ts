// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// @watch start
// templates/shared/search/issue/syntax.tmpl
// web_src/css/modules/dialog.css
// @watch end

import {type Locator, expect} from '@playwright/test';
import {test} from './utils_e2e.ts';

for (const run of [
  {title: 'JS off', useJs: false},
  {title: 'JS on', useJs: true},
]) {
  test.describe(`Issue search (${run.title})`, () => {
    test.use({javaScriptEnabled: run.useJs});

    const launchInteractions = [
      {kind: 'click', do: (l: Locator) => l.click()},
      {kind: 'spacebar', do: (l: Locator) => l.press(' ')},
      {kind: 'enter key', do: (l: Locator) => l.press('Enter')},
    ];
    for (const interaction of launchInteractions) {
      test(`info modal appears on button + ${interaction.kind}`, async ({page}) => {
        const response = await page.goto('/user2/repo1/issues', {waitUntil: 'domcontentloaded'});
        expect(response?.status()).toBe(200);

        const search = page.locator('form.issue-list-search');
        await expect(search).toBeVisible();

        const syntaxModal = page.locator('#search-syntax-modal');
        await expect(syntaxModal).toBeHidden();

        await interaction.do(search.locator('button[command="show-modal"]'));
        await expect(syntaxModal).toBeVisible();
      });
    }

    test('info modal disappears on Esc', async ({page}) => {
      const response = await page.goto('/user2/repo1/issues', {waitUntil: 'domcontentloaded'});
      expect(response?.status()).toBe(200);

      const search = page.locator('form.issue-list-search');
      await expect(search).toBeVisible();

      const syntaxModal = page.locator('#search-syntax-modal');
      await expect(syntaxModal).toBeHidden();
      await search.locator('button[command="show-modal"]').click();
      await expect(syntaxModal).toBeVisible();

      await page.keyboard.press('Escape');
      await expect(syntaxModal).toBeHidden();
    });

    test('info modal disappears on Cancel button', async ({page, isMobile}) => {
      const response = await page.goto('/user2/repo1/issues', {waitUntil: 'domcontentloaded'});
      expect(response?.status()).toBe(200);

      const search = page.locator('form.issue-list-search');
      await expect(search).toBeVisible();

      const syntaxModal = page.locator('#search-syntax-modal');
      await expect(syntaxModal).toBeHidden();
      await search.locator('button[command="show-modal"]').click();
      await expect(syntaxModal).toBeVisible();

      await syntaxModal.getByText('Cancel').click({force: isMobile}); // dl intercepts pointer events on mobile somehow
      await expect(syntaxModal).toBeHidden();
    });

    test('info modal disappears on click outside', async ({page}) => {
      const response = await page.goto('/user2/repo1/issues', {waitUntil: 'domcontentloaded'});
      expect(response?.status()).toBe(200);

      const search = page.locator('form.issue-list-search');
      await expect(search).toBeVisible();

      const syntaxModal = page.locator('#search-syntax-modal');
      await expect(syntaxModal).toBeHidden();
      await search.locator('button[command="show-modal"]').click();
      await expect(syntaxModal).toBeVisible();

      const box = await syntaxModal.boundingBox();
      await page.mouse.click(box.x + 2, box.y + 2); // clicking the modal itself does nothing
      await expect(syntaxModal).toBeVisible();
      await page.mouse.click(box.x - 1, box.y);
      await expect(syntaxModal).toBeHidden();
    });
  });
}
