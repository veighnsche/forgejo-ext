// Copyright 2025 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// @watch start
// templates/repo/commit_header.tmpl
// @watch end

import {expect} from '@playwright/test';
import {dynamic_id, test} from './utils_e2e.ts';
import {screenshot} from './shared/screenshots.ts';

test.use({user: 'user2'});

test('Create branch from commit', async ({page}) => {
  let response = await page.goto('/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d');
  expect(response?.status()).toBe(200);

  // Open create branch modal.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Create branch'}).click();
  await expect(page.locator('#create-branch-modal')).toBeVisible();
  await screenshot(page, page.locator('#create-branch-modal'));

  // Check that it can be cancelled.
  await page.getByRole('button', {name: 'Cancel'}).click();
  await expect(page.locator('#create-branch-modal')).toBeHidden();

  // Open it again and make a branch.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Create branch'}).click();
  await expect(page.locator('#create-branch-modal')).toBeVisible();

  const branchName = dynamic_id();
  await page.getByRole('textbox').fill(branchName);
  await page.getByRole('button', {name: 'Create branch'}).click();

  // Verify branch exists.
  response = await page.goto(`/user2/repo1/src/branch/${branchName}`);
  expect(response?.status()).toBe(200);
});

test('Create tag from commit', async ({page}) => {
  let response = await page.goto('/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d');
  expect(response?.status()).toBe(200);

  // Open create tag modal.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Create tag'}).click();
  await expect(page.locator('#create-tag-modal')).toBeVisible();
  await screenshot(page, page.locator('#create-tag-modal'));

  // Check that it can be cancelled.
  await page.getByRole('button', {name: 'Cancel'}).click();
  await expect(page.locator('#create-tag-modal')).toBeHidden();

  // Open it again and make a branch.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Create tag'}).click();
  await expect(page.locator('#create-tag-modal')).toBeVisible();

  const tagName = dynamic_id();
  await page.getByRole('textbox').fill(tagName);
  await page.getByRole('button', {name: 'Create tag'}).click();

  // Verify tag exists.
  response = await page.goto(`/user2/repo1/releases/tag/${tagName}`);
  expect(response?.status()).toBe(200);
});

test('Cherry-pick commit and then revert it', async ({page}) => {
  const latestCommit = page.locator('#repo-files-table .commit-list .repo-files-table-latest-commit-cell .commit-summary .message-wrapper .default-link');
  const modal = page.locator('#cherry-pick-modal');
  const menu = page.locator('.js-branch-tag-selector .menu');

  // Navigate to the test repository.
  const response = await page.goto('/user2/cherry-picking/src/branch/main');
  expect(response?.status()).toBe(200);

  // Open the commit we want to cherry-pick.
  await latestCommit.click();

  // Check that the cherry-picking modal can be cancelled.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Cherry-pick'}).click();
  await expect(modal).toBeVisible();
  await page.getByRole('button', {name: 'Cancel'}).click();
  await expect(modal).toBeHidden();

  // Open it again.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Cherry-pick'}).click();
  await expect(page.locator('#cherry-pick-modal')).toBeVisible();

  // Pick the target branch.
  await page.locator('.js-branch-tag-selector button').click();
  await menu.locator('.item').getByText('basket').click();

  // Check that the file introduced in the cherry-picked commit is visible on the target branch.
  await page.getByRole('button', {name: 'Commit changes'}).click();
  await expect(page).toHaveURL('/user2/cherry-picking/src/branch/basket');
  await expect(page.locator('#repo-files-table tbody tr.entry td.name')).toHaveText(['new-cherry.txt', 'old-cherries.txt', 'README.md']);

  // Navigate to the cherry-picked commit and check that its author and committer are correct.
  await latestCommit.click();
  await expect(page.locator('.commit-header-row .author img').first()).toHaveAttribute('title', 'cherryenthusiast@example.com');
  await expect(page.locator('.commit-header-row .author strong').first()).toHaveText('Cherry Enthusiast');
  await expect(page.locator('.commit-header-row .author a strong')).toHaveText('user2');

  // Check that the reverting modal can be cancelled.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Revert'}).click();
  await expect(modal).toBeVisible();
  await page.getByRole('button', {name: 'Cancel'}).click();
  await expect(modal).toBeHidden();

  // Open it again.
  await page.locator('.commit-header-buttons .dropdown.button').click();
  await page.getByRole('option', {name: 'Revert'}).click();
  await expect(page.locator('#cherry-pick-modal')).toBeVisible();

  // Pick the target branch.
  await page.locator('.js-branch-tag-selector button').click();
  await menu.locator('.item').getByText('basket').click();

  // Check that the file added in the just reverted commit is not visible on the target branch.
  await page.getByRole('button', {name: 'Commit changes'}).click();
  await expect(page).toHaveURL('/user2/cherry-picking/src/branch/basket');
  await expect(page.locator('#repo-files-table tbody tr.entry td.name')).toHaveText(['old-cherries.txt', 'README.md']);

  // Navigate to the reversal commit and check that its author is correct.
  await latestCommit.click();
  await expect(page.locator('.commit-header-row .author img')).toHaveAttribute('title', '< U<se>r Tw<o > ><');
  await expect(page.locator('.commit-header-row .author strong')).toHaveText('< U<se>r Tw<o > ><');
});
