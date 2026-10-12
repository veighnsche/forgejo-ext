import {vi} from 'vitest';

import {issueTitleHTML, excludeLabel} from './repo-issue-sidebar-list.ts';
import {
  findWipPrefix,
  initArchivedLabelHandler,
  initRepoIssueBranchSelect,
  initRepoIssueCommentDelete,
  initRepoIssueTitleEdit,
  initRepoIssueWipTitle,
  initRepoPullRequestAllowMaintainerEdit,
  retireRepoIssueBranchSelect,
  retireRepoIssueTitleEdit,
  retireRepoIssueWipTitle,
  retireRepoPullRequestAllowMaintainerEdit,
} from './repo-issue.js';
import {POST} from '../modules/fetch.js';

vi.mock('./comp/ComboMarkdownEditor.js', () => ({}));
// jQuery is missing
vi.mock('./common-global.js', () => ({}));
vi.mock('../modules/fetch.js', () => ({POST: vi.fn(), GET: vi.fn()}));

afterEach(() => {
  retireRepoIssueWipTitle();
  retireRepoPullRequestAllowMaintainerEdit();
  retireRepoIssueTitleEdit();
  retireRepoIssueBranchSelect();
  document.body.innerHTML = '';
});

test('Convert issue title to html', () => {
  expect(issueTitleHTML('')).toEqual('');
  expect(issueTitleHTML('issue title')).toEqual('issue title');

  const expected_thumbs_up = `<span class="emoji" title=":+1:">👍</span>`;
  expect(issueTitleHTML(':+1:')).toEqual(expected_thumbs_up);
  expect(issueTitleHTML(':invalid emoji:')).toEqual(':invalid emoji:');

  const expected_code_block = `<code class="inline-code-block">code</code>`;
  expect(issueTitleHTML('`code`')).toEqual(expected_code_block);
  expect(issueTitleHTML('`invalid code')).toEqual('`invalid code');
  expect(issueTitleHTML('invalid code`')).toEqual('invalid code`');

  expect(issueTitleHTML('issue title :+1: `code`')).toEqual(`issue title ${expected_thumbs_up} ${expected_code_block}`);
});

const getLabelsParam = () => new URLSearchParams(window.location.search).get('labels');

test('Toggles label exclusion from filters', () => {
  expect(getLabelsParam()).toEqual(null);

  const element = document.createElement('div');
  element.dataset['label-id'] = '1';

  // excludes it
  excludeLabel(element);
  expect(getLabelsParam()).toEqual('-1');

  // since it was excluded above, now it should delete it
  excludeLabel(element);
  expect(getLabelsParam()).toEqual('');

  // if we add it manually it should swap it to an exclusion
  window.location.search = '?labels=1';
  expect(getLabelsParam()).toEqual('1');
  excludeLabel(element);
  expect(getLabelsParam()).toEqual('-1');
});

test('Finds wip prefix in string', () => {
  const wipPrefixes = ['wIp:', '[WIP]'];

  expect(findWipPrefix('[wIP]', wipPrefixes)).toBe('[WIP]');
  expect(findWipPrefix('[wip]', wipPrefixes)).toBe('[WIP]');
  expect(findWipPrefix('[WIP]', wipPrefixes)).toBe('[WIP]');

  expect(findWipPrefix('wIP:', wipPrefixes)).toBe('wIp:');
  expect(findWipPrefix('wip:', wipPrefixes)).toBe('wIp:');
  expect(findWipPrefix('WIP:', wipPrefixes)).toBe('wIp:');

  expect(findWipPrefix('wIP:', [])).toBe(undefined);
  expect(findWipPrefix('wIP:', [])).toBe(undefined);
  expect(findWipPrefix('wip:', [])).toBe(undefined);

  expect(findWipPrefix('wip:', ['[WIP]'])).toBe(undefined);
  expect(findWipPrefix('WIP:', ['[WIP]'])).toBe(undefined);
  expect(findWipPrefix('WIP:', ['[WIP]'])).toBe(undefined);
});

test('maintainer-edit toggle posts once per change and detaches on retire', async () => {
  document.body.innerHTML = `<div id="allow-edits-from-maintainers" data-url="/owner/repo/issues/1" data-prompt-error="err">
    <input type="checkbox">
  </div>`;
  POST.mockResolvedValue({ok: true, json: async () => ({allow_maintainer_edit: true})});
  const checkbox = document.querySelector('input[type="checkbox"]');
  initRepoPullRequestAllowMaintainerEdit();
  initRepoPullRequestAllowMaintainerEdit();
  checkbox.checked = true;
  checkbox.dispatchEvent(new Event('input'));
  await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
  expect(POST).toHaveBeenCalledWith('/owner/repo/issues/1/set_allow_maintainer_edit', {data: new URLSearchParams({allow_maintainer_edit: 'true'})});

  retireRepoPullRequestAllowMaintainerEdit();
  checkbox.dispatchEvent(new Event('input'));
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(POST).toHaveBeenCalledTimes(1);
});

test('wip title link prefixes the current title once and detaches on retire', () => {
  document.body.innerHTML = `<div class="title_wip_desc" data-wip-prefixes='["[WIP]"]'><a href="#">mark wip</a></div>
  <input id="issue_title" value="hello">`;
  const input = document.getElementById('issue_title');
  let focuses = 0;
  input.addEventListener('focus', () => focuses++);
  initRepoIssueWipTitle();
  initRepoIssueWipTitle();
  document.querySelector('.title_wip_desc > a').click();
  expect(input.value).toBe('[WIP] hello');
  expect(focuses).toBe(1);

  retireRepoIssueWipTitle();
  input.value = 'again';
  document.querySelector('.title_wip_desc > a').click();
  expect(input.value).toBe('again');
});

test('comment delete document handler installs once across repeated inits', async () => {
  document.body.innerHTML = `<div id="issuecomment-9"></div>
  <button class="delete-comment" data-locale="Delete?" data-url="/comments/9/delete" data-comment-id="issuecomment-9">x</button>`;
  POST.mockResolvedValue({ok: true});
  window.confirm = vi.fn(() => true);
  try {
    initRepoIssueCommentDelete();
    initRepoIssueCommentDelete();
    document.querySelector('.delete-comment').click();
    await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
    expect(POST).toHaveBeenCalledWith('/comments/9/delete');
    expect(document.getElementById('issuecomment-9')).toBeNull();
  } finally {
    delete window.confirm;
  }
});

test('archived label handler toggles native labels and leaves retained ones alone', () => {
  document.body.innerHTML = `<div class="archived-label-hint"></div>
  <span data-is-archived="true" class="checked" id="native-checked">a</span>
  <span data-is-archived="true" id="native-unchecked">b</span>
  <div data-extension-panel><span data-is-archived="true" id="retained-unchecked">c</span></div>`;
  initArchivedLabelHandler();
  expect(document.getElementById('native-checked').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('native-unchecked').classList.contains('tw-hidden')).toBe(true);
  expect(document.getElementById('retained-unchecked').classList.contains('tw-hidden')).toBe(false);
});

test('title edit show/cancel toggles editor visibility and detaches on retire', () => {
  document.body.innerHTML = `<div id="issue-title-display"><button id="issue-title-edit-show" type="button">edit</button></div>
  <div id="issue-title-editor" class="tw-hidden">
    <input data-old-title="Old title" value="">
    <button class="ui cancel button" type="button">Cancel</button>
    <button class="ui primary button" type="button" data-update-url="/title">Save</button>
  </div>`;
  const editor = document.getElementById('issue-title-editor');
  initRepoIssueTitleEdit();
  initRepoIssueTitleEdit();
  document.getElementById('issue-title-edit-show').click();
  expect(editor.classList.contains('tw-hidden')).toBe(false);
  document.querySelector('.ui.cancel.button').click();
  expect(editor.classList.contains('tw-hidden')).toBe(true);

  retireRepoIssueTitleEdit();
  document.getElementById('issue-title-edit-show').click();
  expect(editor.classList.contains('tw-hidden')).toBe(true);
});

test('branch select rewrites the PR target once and detaches on retire', () => {
  document.body.innerHTML = `<div id="branch-select"><div class="item" data-branch="feature">feature</div></div>
  <span id="pull-target-branch" data-basename="owner" data-branch="main">owner:main</span>`;
  const target = document.getElementById('pull-target-branch');
  initRepoIssueBranchSelect();
  initRepoIssueBranchSelect();
  document.querySelector('.item[data-branch]').click();
  expect(target.getAttribute('data-branch')).toBe('feature');
  expect(target.textContent).toBe('owner:feature');

  retireRepoIssueBranchSelect();
  target.setAttribute('data-branch', 'main');
  target.textContent = 'owner:main';
  document.querySelector('.item[data-branch]').click();
  expect(target.getAttribute('data-branch')).toBe('main');
});
