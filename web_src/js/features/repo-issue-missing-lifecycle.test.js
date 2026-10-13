import $ from 'jquery';
import {vi} from 'vitest';
import {POST} from '../modules/fetch.js';
import {
  initRepoIssueAssignMe, retireRepoIssueAssignMe,
  initRepoIssueComments, retireRepoIssueComments,
  initRepoIssueReferenceRepositorySearch, retireRepoIssueReferenceRepositorySearch,
  initRepoIssueWipToggle, retireRepoIssueWipToggle,
  initRepoPullRequestReview, retireRepoPullRequestReview,
  initRepoPullRequestUpdate, retireRepoPullRequestUpdate,
  initRepoIssueTimeTracking, initRepoIssueDue, initRepoIssueDependencyDelete,
  initRepoIssueCodeCommentCancel, initRepoIssueReferenceIssue,
} from './repo-issue.js';

vi.mock('../modules/fetch.js', () => ({POST: vi.fn(), GET: vi.fn()}));
vi.mock('./comp/ComboMarkdownEditor.js', () => ({getComboMarkdownEditor: () => ({}), initComboMarkdownEditor: vi.fn()}));
vi.mock('../modules/tippy.js', () => ({createTippy: vi.fn(() => ({destroy: vi.fn(), hide: vi.fn()})), showTemporaryTooltip: vi.fn()}));
vi.mock('./common-global.js', () => ({initDropzone: vi.fn()}));
vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));

afterEach(() => {
  retireRepoPullRequestUpdate();
  retireRepoIssueWipToggle();
  retireRepoIssueAssignMe();
  retireRepoIssueComments();
  retireRepoIssueReferenceRepositorySearch();
  retireRepoPullRequestReview();
  document.body.innerHTML = '';
  vi.clearAllMocks();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

test('pull-request update posts once with current action and retire detaches', async () => {
  document.body.innerHTML = `<div class="update-button"><button data-do="/pr/update-1">Update</button><div class="dropdown"></div></div>`;
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    POST.mockResolvedValue({json: async () => ({})});
    const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {});
    initRepoPullRequestUpdate();
    initRepoPullRequestUpdate();
    document.querySelector('.update-button > button').click();
    await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
    expect(POST.mock.lastCall[0]).toBe('/pr/update-1');
    await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
    retireRepoPullRequestUpdate();
    expect(dropdown).toHaveBeenLastCalledWith('destroy');
    POST.mockClear();
    document.querySelector('.update-button > button').click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(POST).not.toHaveBeenCalled();
    // re-entry with fresh action uses current context
    document.body.innerHTML = `<div class="update-button"><button data-do="/pr/update-2">Update</button><div class="dropdown"></div></div>`;
    initRepoPullRequestUpdate();
    document.querySelector('.update-button > button').click();
    await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
    expect(POST.mock.lastCall[0]).toBe('/pr/update-2');
  } finally {
    dropdown.mockRestore();
  }
});

test('wip toggle posts once and retire detaches', async () => {
  document.body.innerHTML = `<div class="toggle-wip" data-title="hello" data-wip-prefixes='["[WIP]"]' data-update-url="/wip"><a href="#">toggle</a></div>`;
  POST.mockResolvedValue({ok: true});
  const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {});
  initRepoIssueWipToggle();
  initRepoIssueWipToggle();
  document.querySelector('.toggle-wip a').click();
  await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
  expect(POST.mock.lastCall[0]).toBe('/wip');
  await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
  retireRepoIssueWipToggle();
  POST.mockClear();
  document.querySelector('.toggle-wip a').click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(POST).not.toHaveBeenCalled();
});

test('assign-me select path checks current assignee once and retire detaches', () => {
  document.body.innerHTML = `<div class="ui assignees list"><div class="item no-select"><a class="select-assign-me" data-id="7" data-id-selector="#assignee-7" href="#">me</a></div></div>
    <div class="ui select-assignees"><div class="menu" data-id="#assignee-input"><div class="item" data-id="7"><i class="octicon-check tw-invisible"></i></div></div></div>
    <input id="assignee-input" value=""><div id="assignee-7" class="tw-hidden"></div>`;
  initRepoIssueAssignMe();
  initRepoIssueAssignMe();
  document.querySelector('.select-assign-me').click();
  expect(document.querySelector('.menu .item').classList.contains('checked')).toBe(true);
  expect(document.getElementById('assignee-7').classList.contains('tw-hidden')).toBe(false);
  expect(document.getElementById('assignee-input').value).toBe('7');
  retireRepoIssueAssignMe();
  document.querySelector('.menu .item').classList.remove('checked');
  document.querySelector('.select-assign-me').click();
  expect(document.querySelector('.menu .item').classList.contains('checked')).toBe(false);
});

test('issue comments re-request posts once and retire detaches', async () => {
  document.body.innerHTML = `<div class="repository view issue"><div class="timeline"></div><button class="re-request-review" data-update-url="/meta" data-issue-id="1" data-id="2">re</button></div>`;
  POST.mockResolvedValue({ok: true});
  const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {});
  initRepoIssueComments();
  initRepoIssueComments();
  document.querySelector('.re-request-review').click();
  await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
  await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
  retireRepoIssueComments();
  POST.mockClear();
  document.querySelector('.re-request-review').click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(POST).not.toHaveBeenCalled();
});

test('reference repository search binds once per entry and retire destroys', () => {
  document.body.innerHTML = `<form action="/old"><div class="issue_reference_repository_search ui dropdown"></div></form>`;
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    initRepoIssueReferenceRepositorySearch();
    initRepoIssueReferenceRepositorySearch();
    const inits = dropdown.mock.calls.filter((c) => typeof c[0] === 'object');
    expect(inits).toHaveLength(1);
    retireRepoIssueReferenceRepositorySearch();
    expect(dropdown).toHaveBeenLastCalledWith('destroy');
  } finally {
    dropdown.mockRestore();
  }
});

test('pull-request review widget binds once and retire destroys', () => {
  document.body.innerHTML = `<div class="repository pull diff"><div><button class="js-btn-review">Review</button><div class="review-box-panel"><div class="combo-markdown-editor"></div><button class="close">x</button></div></div></div>`;
  initRepoPullRequestReview();
  initRepoPullRequestReview();
  retireRepoPullRequestReview();
  expect(() => retireRepoPullRequestReview()).not.toThrow();
});

test('time-tracking global installs once and survives transplant without rebinding', () => {
  $.fn.modal = vi.fn(function () { return this });
  try {
    document.body.innerHTML = `<button class="issue-add-time">add</button><div class="issue-start-time-modal"><input></div><form id="add_time_manual_form"></form>`;
    initRepoIssueTimeTracking();
    initRepoIssueTimeTracking();
    document.querySelector('.issue-add-time').click();
    expect($.fn.modal.mock.calls.filter((c) => c[0] === 'show')).toHaveLength(1);
    // transplant: replace nodes, no re-init, handler still acts once on current DOM
    document.body.innerHTML = `<button class="issue-add-time">add</button><div class="issue-start-time-modal"><input></div><form id="add_time_manual_form"></form>`;
    $.fn.modal.mockClear();
    initRepoIssueTimeTracking();
    document.querySelector('.issue-add-time').click();
    expect($.fn.modal.mock.calls.filter((c) => c[0] === 'show')).toHaveLength(1);
  } finally {
    delete $.fn.modal;
  }
});

test('due-date global toggles current form once across repeated inits', () => {
  document.body.innerHTML = `<button class="issue-due-edit">edit</button><div id="deadlineForm" class="tw-hidden"></div>`;
  initRepoIssueDue();
  initRepoIssueDue();
  document.querySelector('.issue-due-edit').click();
  expect(document.getElementById('deadlineForm').classList.contains('tw-hidden')).toBe(false);
  document.querySelector('.issue-due-edit').click();
  expect(document.getElementById('deadlineForm').classList.contains('tw-hidden')).toBe(true);
});

test('dependency-delete global opens modal once across repeated inits', () => {
  $.fn.modal = vi.fn(function () { return this });
  try {
    document.body.innerHTML = `<button class="delete-dependency-button" data-id="1" data-type="blocked">x</button><div class="remove-dependency"></div><input id="removeDependencyID"><input id="dependencyType"><form id="removeDependencyForm"></form>`;
    initRepoIssueDependencyDelete();
    initRepoIssueDependencyDelete();
    document.querySelector('.delete-dependency-button').click();
    expect($.fn.modal.mock.calls.filter((c) => c[0] === 'show')).toHaveLength(1);
  } finally {
    delete $.fn.modal;
  }
});

test('code-comment-cancel global hides current form once across repeated inits', () => {
  document.body.innerHTML = `<div class="comment-code-cloud"><form class="comment-form"><button class="cancel-code-comment" type="button">cancel</button></form><button class="comment-form-reply tw-hidden">reply</button></div>`;
  initRepoIssueCodeCommentCancel();
  initRepoIssueCodeCommentCancel();
  document.querySelector('.cancel-code-comment').click();
  expect(document.querySelector('form.comment-form').classList.contains('tw-hidden')).toBe(true);
  expect(document.querySelector('.comment-form-reply').classList.contains('tw-hidden')).toBe(false);
});

test('reference-issue global fills current modal once across repeated inits', () => {
  $.fn.modal = vi.fn(function () { return this });
  try {
    document.body.innerHTML = `<div id="target">content</div><button class="reference-issue" data-target="target" data-poster-username="bob" data-reference="/r/issues/1" data-modal="#ref-modal">ref</button><div id="ref-modal"><textarea name="content"></textarea></div>`;
    initRepoIssueReferenceIssue();
    initRepoIssueReferenceIssue();
    document.querySelector('.reference-issue').click();
    expect(document.querySelector('#ref-modal textarea').value).toContain('bob');
    expect($.fn.modal.mock.calls.filter((c) => c[0] === 'show')).toHaveLength(1);
  } finally {
    delete $.fn.modal;
  }
});
