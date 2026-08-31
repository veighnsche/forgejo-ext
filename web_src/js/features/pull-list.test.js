// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

import {vi} from 'vitest';
import {POST} from '../modules/fetch.js';
import {initCommonIssueListSelection} from './common-issue-list.js';
import {initPullRequestListMerge, mergePullRequestsSequentially} from './pull-list.js';

vi.mock('../modules/fetch.js', () => ({POST: vi.fn(), GET: vi.fn()}));

const pulls = [
  {url: '/owner/first/pulls/1/merge-from-list', headCommitId: 'first-sha', baseBranch: 'main'},
  {url: '/owner/second/pulls/1/merge-from-list', headCommitId: 'second-sha', baseBranch: 'release'},
  {url: '/owner/first/pulls/2/merge-from-list', headCommitId: 'third-sha', baseBranch: 'main'},
];
const options = {mergeStyle: 'squash', deleteBranch: true, continueOnFailure: false};
const response = (body, status = 200) => new Response(JSON.stringify(body), {status});

beforeEach(() => {
  vi.clearAllMocks();
  POST.mockReset();
  document.body.replaceChildren();
});

test('merges across repositories one at a time using the confirmed source and target', async () => {
  let resolveFirst;
  POST.mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
    .mockResolvedValueOnce(response({merged: true}));
  const onResult = vi.fn();
  const processing = mergePullRequestsSequentially(pulls.slice(0, 2), options, onResult);
  expect(POST).toHaveBeenCalledTimes(1);
  expect(POST).toHaveBeenNthCalledWith(1, pulls[0].url, {
    data: {do: 'squash', head_commit_id: 'first-sha', base_branch: 'main', delete_branch_after_merge: true},
    redirect: 'error',
  });

  resolveFirst(response({merged: true}));
  const results = await processing;
  expect(POST).toHaveBeenCalledTimes(2);
  expect(POST).toHaveBeenNthCalledWith(2, pulls[1].url, {
    data: {do: 'squash', head_commit_id: 'second-sha', base_branch: 'release', delete_branch_after_merge: true},
    redirect: 'error',
  });
  expect(results.map((result) => result.status)).toEqual(['merged', 'merged']);
  expect(onResult.mock.calls.map(([pull, result]) => [pull.url, result.status])).toEqual([
    [pulls[0].url, 'running'], [pulls[0].url, 'merged'], [pulls[1].url, 'running'], [pulls[1].url, 'merged'],
  ]);
});

test('stops after a rejected merge by default, without sending the remaining requests', async () => {
  POST.mockResolvedValueOnce(response({merged: false, message: 'Merge conflict'}, 409));
  const results = await mergePullRequestsSequentially(pulls, options, vi.fn());
  expect(results).toEqual([{status: 'skipped', message: 'Merge conflict'}, {status: 'stopped'}, {status: 'stopped'}]);
  expect(POST).toHaveBeenCalledTimes(1);
});

test('continues after a known failure when selected and preserves a successful merge with a deletion warning', async () => {
  POST.mockResolvedValueOnce(response({merged: false, message: 'Required checks missing'}, 409))
    .mockResolvedValueOnce(response({merged: true, warning: 'No permission to delete source branch'}));
  const results = await mergePullRequestsSequentially(pulls.slice(0, 2), {...options, continueOnFailure: true}, vi.fn());
  expect(results).toEqual([
    {status: 'skipped', message: 'Required checks missing'},
    {status: 'merged', warning: 'No permission to delete source branch'},
  ]);
  expect(POST).toHaveBeenCalledTimes(2);
});

test('does not retry or continue when the network result is unknown', async () => {
  POST.mockRejectedValueOnce(new Error('Connection lost'));
  const results = await mergePullRequestsSequentially(pulls, {...options, continueOnFailure: true}, vi.fn());
  expect(results.map((result) => result.status)).toEqual(['unknown', 'stopped', 'stopped']);
  expect(POST).toHaveBeenCalledTimes(1);
});

test.each([
  [500, '{"merged":false,"message":"Internal error"}'],
  [408, 'Request timed out'],
  [200, '<html>Login page</html>'],
  [200, '{"redirect":"/owner/repo/pulls/1"}'],
])('stops on an ambiguous HTTP %d response', async (status, body) => {
  POST.mockResolvedValueOnce(new Response(body, {status}));
  const results = await mergePullRequestsSequentially(pulls, {...options, continueOnFailure: true}, vi.fn());
  expect(results.map((result) => result.status)).toEqual(['unknown', 'stopped', 'stopped']);
  expect(POST).toHaveBeenCalledTimes(1);
});

test('handles a non-JSON authorization error as a rejection', async () => {
  POST.mockResolvedValueOnce(new Response('Forbidden', {status: 403}));
  const results = await mergePullRequestsSequentially(pulls, options, vi.fn());
  expect(results[0]).toEqual({status: 'skipped', message: ''});
});

test('does not merge before confirmation and ignores repeated confirmation while running', async () => {
  document.body.innerHTML = '<input type="checkbox" class="issue-checkbox-all">' +
    '<button id="pull-list-merge-button" data-label="Merge selected (%d)" data-unavailable="Unavailable"><span></span></button>' +
    '<button id="pull-list-merge-clear"></button>' +
    '<div id="issue-list"><div class="flex-item">' +
    '<input type="checkbox" class="issue-checkbox" data-pull-merge-url="/owner/repo/pulls/1/merge-from-list" data-head-commit-id="sha" data-base-branch="main" data-pull-label="owner/repo#1">' +
    '<a class="issue-title" href="/owner/repo/pulls/1">A safe title</a></div></div>' +
    '<dialog id="pull-list-merge-dialog" data-waiting="Waiting" data-running="Merging" data-merged="Merged" data-summary="%[1]d merged, %[2]d skipped, %[3]d stopped, %[4]d unknown" data-cancel="Cancel" data-close="Close">' +
    '<form><select name="do"><option value="default">Default</option></select><input type="checkbox" name="delete_branch_after_merge"><input type="checkbox" name="continue_on_failure"></form>' +
    '<ol class="pull-list-merge-results"></ol><p id="pull-list-merge-summary"></p><button class="ok"></button><button class="cancel"></button><button class="reload"></button></dialog>';
  const dialog = document.querySelector('dialog');
  vi.spyOn(dialog, 'showModal').mockImplementation(() => { dialog.open = true });
  const checkbox = document.querySelector('.issue-checkbox');
  const button = document.querySelector('#pull-list-merge-button');
  const confirm = dialog.querySelector('.ok');
  initCommonIssueListSelection();
  initPullRequestListMerge();
  expect(button.disabled).toBe(true);

  checkbox.click();
  expect(button.disabled).toBe(false);
  button.click();
  expect(dialog.open).toBe(true);
  expect(POST).not.toHaveBeenCalled();

  let resolveMerge;
  POST.mockImplementationOnce(() => new Promise((resolve) => { resolveMerge = resolve }));
  confirm.click();
  confirm.click();
  expect(POST).toHaveBeenCalledTimes(1);
  expect(dialog.querySelector('.cancel').disabled).toBe(true);
  const escape = new Event('cancel', {cancelable: true});
  dialog.dispatchEvent(escape);
  expect(escape.defaultPrevented).toBe(true);

  resolveMerge(response({merged: true}));
  await vi.waitFor(() => expect(dialog.querySelector('.cancel').disabled).toBe(false));
  expect(checkbox.checked).toBe(false);
  expect(checkbox.disabled).toBe(true);
  expect(dialog.querySelector('#pull-list-merge-summary').textContent).toBe('1 merged, 0 skipped, 0 stopped, 0 unknown');
  expect(POST).toHaveBeenCalledTimes(1);
});
