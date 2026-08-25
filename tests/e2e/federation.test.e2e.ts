// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// @watch start
// templates/repo/migrate/federated.tmpl
// templates/repo/migrate/migrate.tmpl
// templates/user/profile.tmpl
// templates/shared/user/profile_big_avatar.tmpl
// templates/explore/users.tmpl
// templates/explore/user_list.tmpl
// templates/admin/config.tmpl
// @watch end

import {expect} from '@playwright/test';
import {test} from './utils_e2e.ts';
import {screenshot} from './shared/screenshots.ts';

test.use({user: 'user2'});

test('Federated repository mirror migration form', async ({page}) => {
  const response = await page.goto('/repo/federated-mirror');
  expect(response?.status()).toBe(200);

  const form = page.locator('form');
  await expect(form).toBeVisible();

  const remoteActorInput = form.locator('#remote_actor_uri');
  await expect(remoteActorInput).toBeVisible();
  await expect(remoteActorInput).toHaveAttribute('required', '');

  const repoNameInput = form.locator('#repo_name');
  await expect(repoNameInput).toBeVisible();

  const submitButton = form.locator('button.primary');
  await expect(submitButton).toBeVisible();

  await screenshot(page);
});

test('Migration select screen shows federated entry', async ({page}) => {
  const response = await page.goto('/repo/migrate');
  expect(response?.status()).toBe(200);

  const fedEntry = page.locator('a[href*="/repo/federated-mirror"]');
  await expect(fedEntry).toBeVisible();

  await screenshot(page);
});

test('Explore users search interface', async ({page}) => {
  const response = await page.goto('/explore/users');
  expect(response?.status()).toBe(200);

  const searchInput = page.locator('input[name="q"]');
  await expect(searchInput).toBeVisible();

  await page.goto('/explore/users?q=user1');
  await expect(page.locator('.flex-list')).toBeVisible();

  await screenshot(page);
});

test.describe('Admin federation view', () => {
  test.use({user: 'user1'});

  test('Admin configuration shows federation section', async ({page}) => {
    const response = await page.goto('/admin/config');
    expect(response?.status()).toBe(200);

    await expect(page.getByText('Federation configuration')).toBeVisible();
    await expect(page.getByText('Require HTTP signatures')).toBeVisible();

    await screenshot(page);
  });
});
