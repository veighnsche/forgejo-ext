// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// @watch start
// templates/repo/commits_list.tmpl
// templates/repo/latest_commit.tmpl
// templates/repo/pulls/commits_list.tmpl
// web_src/css/repo.css
// web_src/css/repo/commit-list.css
// @watch end

import {expect} from '@playwright/test';
import {test} from './utils_e2e.ts';
import {screenshot} from './shared/screenshots.ts';

const runs = [
  {title: 'JS off', useJs: false},
  {title: 'JS on', useJs: true},
] as const;
for (const run of runs) {
  test.describe(`Commits list (${run.title})`, () => {
    test.use({javaScriptEnabled: run.useJs});

    test('Repo latest commit', async ({page, isMobile}) => {
      const response = await page.goto('/user2/mentions-highlighted');
      expect(response?.status()).toBe(200);

      const summary = page.locator('.commit-summary', {hasText: 'Another commit which mentions @user1 in the title'});
      const toggle = summary.getByLabel('Toggle full commit message');
      const body = page.locator('.commit-body', {hasText: 'and @user2 in the text'})

      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();

      await toggle.click({force: isMobile}); // open!
      await expect(toggle).toBeVisible();
      await expect(summary).toBeVisible();
      await expect(body).toBeVisible();

      await toggle.click({force: isMobile}); // close!
      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();
    });

    test('Repo commits', async ({page, isMobile}) => {
      const response = await page.goto('/user2/mentions-highlighted/commits/branch/main');
      expect(response?.status()).toBe(200);

      const summary = page.locator('.message-wrapper', {hasText: 'Another commit which mentions @user1 in the title'});
      const toggle = summary.locator('+ details').getByLabel('Toggle full commit message');
      const body = page.locator('.commit-body', {hasText: 'and @user2 in the text'})
      const otherBody = page.locator('.commit-body', {hasText: 'and has some additional text which mentions @user1'})

      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();
      await expect(otherBody).toBeHidden();

      await toggle.click({force: isMobile}); // open!
      await expect(toggle).toBeVisible();
      await expect(summary).toBeVisible();
      await expect(body).toBeVisible();
      await expect(otherBody).toBeHidden();

      await toggle.click({force: isMobile}); // close!
      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();
      await expect(otherBody).toBeHidden();

      // TODO: Ensure statuses and tags also work the same way; need a fixture with both statuses and a multiline message
    });
  });
}

test.describe('PR commits', () => {
  test.use({user: 'user2'});

  test('Any layout', async ({page}) => {
    const response = await page.goto('/user2/repo1/pulls/3/commits');
    expect(response?.status()).toBe(200);

    const commitGroup = page.locator('.commit-group:first-of-type');

    // Date group visibility test
    await expect(commitGroup).toBeVisible();
    await expect(commitGroup.locator('h4')).toBeVisible();

    const commit = commitGroup.locator('.commit:first-child');

    await expect(commit).toHaveCSS('display', 'grid');
  });

  test('Mobile responsive layout checks', async ({page, isMobile}) => {
    test.skip(!isMobile);

    const response = await page.goto('/user2/repo1/pulls/3/commits');
    expect(response?.status()).toBe(200);

    // Mobile-specific visibility test
    const commit = page.locator('.commit-group:first-of-type .commit:first-child');
    await expect(commit.locator('.commit-buttons')).toBeHidden();
    await expect(commit.locator('.button-sequence button[data-clipboard-text]')).toBeVisible();

    // Mobile-specific grid positioning
    // toHaveCSS returns absolute values in px with decimals. This matcher only
    // checks if the string has two \S+px separated by one \s+
    await expect(commit).toHaveCSS('grid-template-columns', /^\S+px\s+\S+px$/);
    await expect(commit.locator('.author')).toHaveCSS('grid-column-start', '1');
    await expect(commit.locator('.date')).toHaveCSS('grid-column-start', '2');
    await expect(commit.locator('.message')).toHaveCSS('grid-column-end', 'span 2');

    // Horizontal scrolling to check for overflow
    await expect(page.locator('.commits').first()).not.toHaveCSS('overflow-x', 'scroll');

    await screenshot(page);
  });

  test('Dropdown check in mobile viewport', async ({page, isMobile}) => {
    test.skip(!isMobile);

    const response = await page.goto('/user2/repo1/pulls/3/commits');
    expect(response?.status()).toBe(200);

    const commit = page.locator('.commit-group:first-of-type .commit:first-child');

    // Click dropdown btn
    const dropdown = commit.locator('.dialog-dropdown');
    await expect(dropdown).toBeVisible();
    await dropdown.locator('.opener').click();

    // List menu items of dropdown
    const menuItem = commit.locator('.dialog-dropdown ul li a'); // repo_path; always visible
    await expect(menuItem).toHaveAttribute('href', '/user2/repo1/src/commit/5f22f7d0d95d614d25a5b68592adb345a4b5c7fd');
    await menuItem.click();
    await page.waitForURL(/.*\/user2\/repo1\/src\/commit\/5f22f7d0d95d614d25a5b68592adb345a4b5c7f/);
    await screenshot(page);
  });

  test('Desktop responsive layout checks', async ({page, isMobile}) => {
    test.skip(isMobile);

    const response = await page.goto('/user2/repo1/pulls/3/commits');
    expect(response?.status()).toBe(200);

    // Desktop-specific visibility test
    const commit = page.locator('.commit-group:first-of-type .commit:first-child');
    await expect(commit.locator('.commit-buttons')).toBeVisible();
    await expect(commit.locator('.button-sequence button[data-clipboard-text]')).toBeHidden();
    await expect(commit.locator('.dialog-dropdown')).toBeHidden();

    // Desktop layout is has specific grid-template-columns
    // toHaveCSS returns absolute values in px with decimals. This matcher only
    // checks if the string has five \S+px separated by four \s+
    await expect(commit).toHaveCSS('grid-template-columns', /^\S+px\s+\S+px\s+\S+px\s+\S+px\s+\S+px$/);

    await screenshot(page);
  });

  for (const run of runs) {
    test(`Multiline commit message (${run.title})`, async ({browser, page: jsPage, isMobile}) => {
      let response = await jsPage.goto('/user2/mentions-highlighted/pulls/1');
      if (response?.status() === 404) {
        // start a new pull request
        // FIXME: this would be better as a fixture of some kind
        response = await jsPage.goto('/user2/mentions-highlighted/_edit/main/README.md');
        expect(response?.status()).toBe(200);
        await jsPage.locator('.cm-content').click();
        await jsPage.keyboard.press('Control+a');
        await jsPage.keyboard.insertText('test-commit-list-pr-multiline-messages');
        await jsPage.locator('input[name="commit_summary"]').fill('This commit message');
        await jsPage.locator('textarea[name="commit_message"]').fill('contains multiple lines.');
        await jsPage.locator('input[type="radio"][value="commit-to-new-branch"]').click();
        await jsPage.locator('.commit-form-wrapper button[type="submit"]').click();
        await jsPage.waitForURL('/user2/mentions-highlighted/compare/main...user2-patch-1', {waitUntil: 'domcontentloaded'});
        await jsPage.locator('button.show-form', {hasText: 'New pull request'}).click();
        await jsPage.locator('button', {hasText: 'Create pull request'}).click();
        await jsPage.waitForURL('/user2/mentions-highlighted/pulls/1', {waitUntil: 'domcontentloaded'});
      }

      // ensure multiline commits work in the PR view
      const context = await browser.newContext({javaScriptEnabled: run.useJs});
      const page = await context.newPage();
      response = await page.goto('/user2/mentions-highlighted/pulls/1/commits');
      expect(response?.status()).toBe(200);

      const summary = page.locator('.message-wrapper', {hasText: 'This commit message'});
      const toggle = summary.getByLabel('Toggle full commit message');
      const body = page.locator('.commit-body', {hasText: 'contains multiple lines'})

      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();

      await toggle.click({force: isMobile}); // open!
      await expect(toggle).toBeVisible();
      await expect(summary).toBeVisible();
      await expect(body).toBeVisible();

      await toggle.click({force: isMobile}); // close!
      await expect(summary).toBeVisible();
      await expect(toggle).toBeVisible();
      await expect(body).toBeHidden();

      // TODO: Ensure statuses and tags also work the same way; need a fixture with both statuses and a multiline message
    });
  }
});
