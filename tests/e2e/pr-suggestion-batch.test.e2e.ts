// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// @watch start
// templates/repo/diff/suggestion_diffs.tmpl
// templates/repo/diff/comments.tmpl
// templates/repo/diff/suggestion_apply_modal.tmpl
// web_src/js/features/repo-suggestion.js
// web_src/js/features/repo-legacy.js
// routers/web/repo/pull_review.go
// @watch end

import {expect} from '@playwright/test';
import {test, create_temp_user, dynamic_id} from './utils_e2e.ts';

// Creates a code-comment carrying a single ```suggestion block on a proposed-side line, via the Files-changed UI.
async function addSuggestion(page, lineText: string, body: string) {
  const row = page.getByText(lineText, {exact: true}).locator('xpath=ancestor::tr[1]');
  await row.locator('button.add-code-comment').click();
  const form = row.locator('xpath=following-sibling::tr[1]').locator('.comment-code-cloud form');
  await form.locator('textarea.markdown-text-editor').fill(body);
  await form.getByText('Add single comment').click();
}

test('PR: batch apply suggestions (create, edit, batch, discard, apply)', async ({browser, request}, workerInfo) => {
  const {context, username} = await create_temp_user(browser, workerInfo, request);
  const page = await context.newPage();
  const auth = {Authorization: `Basic ${btoa(`${username}:password`)}`};
  const jsonAuth = {...auth, 'Content-Type': 'application/json'};

  const repo = dynamic_id();
  const api = `/api/v1/repos/${username}/${repo}`;
  const created = await request.post('/api/v1/user/repos', {headers: jsonAuth, data: {name: repo, auto_init: true}});
  expect(created.ok()).toBeTruthy();
  const base = (await created.json()).default_branch;

  try {
    const head = 'pr-head';
    expect((await request.post(`${api}/branches`, {headers: jsonAuth, data: {new_branch_name: head, old_branch_name: base}})).ok()).toBeTruthy();

    const fileContent = `${Array.from({length: 60}, (_, i) => `Line ${i + 1}`).join('\n')}\n`;
    expect((await request.post(`${api}/contents/file.md`, {headers: jsonAuth, data: {branch: head, message: 'add file.md', content: btoa(fileContent)}})).ok()).toBeTruthy();

    const pr = await request.post(`${api}/pulls`, {headers: jsonAuth, data: {head, base, title: 'batch test'}});
    expect(pr.ok()).toBeTruthy();
    const prNumber = (await pr.json()).number;

    await page.goto(`/${username}/${repo}/pulls/${prNumber}/files`);

    await addSuggestion(page, 'Line 20', '```suggestion\nLine 20--batched\n```');
    const firstDiff = page.locator('.suggestion-diff').first();
    await expect(firstDiff).toBeVisible();
    await expect(firstDiff.locator('tr.del-code')).toContainText('Line 20');
    await expect(firstDiff.locator('tr.add-code')).toContainText('Line 20--batched');

    await addSuggestion(page, 'Line 50', '```suggestion\nLine 50--batched\n```');
    await expect(page.locator('.suggestion-diff')).toHaveCount(2);
    await expect(page.locator('.add-suggestion-batch')).toHaveCount(2);

    const firstComment = page.locator('.comment').filter({hasText: 'Line 20--batched'});
    await firstComment.locator('details.dropdown summary').click();
    await firstComment.locator('details.dropdown .content .edit-content').click();
    const editZone = firstComment.locator('.edit-content-zone');
    await editZone.locator('textarea.markdown-text-editor').fill('```suggestion\nLine 20--edited\n```');
    await editZone.locator('button[data-button-name="save-edit"]').click();
    await expect(page.locator('.suggestion-diff').first().locator('tr.add-code')).toContainText('Line 20--edited');
    await expect(page.locator('.add-suggestion-batch')).toHaveCount(2);

    // Starting a batch is exclusive: single-applies and the review box hide, the batch bar shows the count.
    const addButtons = page.locator('.add-suggestion-batch');
    const singleApply = page.locator('.apply-suggestion-single').first();
    const reviewBox = page.locator('#review-box');
    const bar = page.locator('#suggestion-batch-bar');
    await addButtons.nth(0).click();
    await addButtons.nth(1).click();
    await expect(singleApply).toBeHidden();
    await expect(reviewBox).toBeHidden();
    await expect(bar).toBeVisible();
    await expect(bar.locator('.batch-count')).toHaveText('2');

    // Discarding the batch restores the single-apply flow.
    await bar.locator('.clear-suggestion-batch').click();
    await expect(bar).toBeHidden();
    await expect(singleApply).toBeVisible();
    await expect(reviewBox).toBeVisible();

    // Rebuild the batch and commit it through the shared apply modal.
    await addButtons.nth(0).click();
    await addButtons.nth(1).click();
    await expect(bar.locator('.batch-count')).toHaveText('2');
    await bar.locator('.commit-suggestion-batch').click();
    const modal = page.locator('#apply-suggestion-modal');
    await expect(modal).toBeVisible();
    await modal.locator('.ok.button').click();

    // The batch lands as a single commit on the head branch, carrying the edited content.
    const headContent = async () => {
      const resp = await request.get(`${api}/contents/file.md?ref=${head}`, {headers: auth});
      return resp.ok() ? atob((await resp.json()).content) : '';
    };
    await expect.poll(headContent).toContain('Line 20--edited');
    const final = await headContent();
    expect(final).toContain('Line 50--batched');
    expect(final).not.toContain('Line 20--batched'); // the pre-edit content was superseded

    const commits = await request.get(`${api}/commits?sha=${head}&limit=2`, {headers: auth});
    expect(commits.ok()).toBeTruthy();
    const [latest, previous] = await commits.json();
    expect(previous.commit.message).toContain('add file.md'); // exactly one commit was added by the batch
    expect(latest.commit.message).not.toContain('add file.md');
  } finally {
    await request.delete(api, {headers: auth});
  }
});
