import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {initRepoDiffConversationNav, retireRepoDiffConversationNav, initRepoDiffView, retireRepoDiffView} from './repo-diff.js';

vi.mock('./repo-diff-filetree.js', () => ({initDiffFileTree: vi.fn(), retireDiffFileTree: vi.fn()}));
vi.mock('./repo-diff-commitselect.js', () => ({initDiffCommitSelect: vi.fn(), retireDiffCommitSelect: vi.fn()}));
vi.mock('./comp/ReactionSelector.js', () => ({initCompReactionSelector: vi.fn()}));
vi.mock('./repo-issue-content.js', () => ({initRepoIssueContentHistory: vi.fn()}));
vi.mock('./comp/ComboMarkdownEditor.js', () => ({validateTextareaNonEmpty: () => true}));
vi.mock('./pull-view-file.js', () => ({initViewedCheckboxListenerFor: vi.fn(), countAndUpdateViewedFiles: vi.fn(), initExpandAndCollapseFilesButton: vi.fn(), retireViewedFiles: vi.fn()}));
vi.mock('./imagediff.js', () => ({initImageDiff: vi.fn(), retireImageDiff: vi.fn()}));
vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));
vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));
vi.mock('htmx.org', () => ({default: {process: vi.fn()}}));

function installDiff() {
  document.body.innerHTML = `<div class="page-content"><div id="diff-file-boxes"></div>
    <button class="file-view-toggle" data-toggle-selector="#view-a">Toggle</button><div id="view-a"></div><div id="view-b" class="tw-hidden"></div>
    <div data-extension-panel><button class="file-view-toggle" data-toggle-selector="#owned-view">Owned</button><div id="owned-view"></div></div></div>`;
}

function installConversationNav() {
  document.body.innerHTML = `<div class="page-content">
    <div class="comment-code-cloud"><div class="comment" id="c1"></div><button class="previous-conversation">Prev</button><button class="next-conversation">Next</button></div>
    <div class="comment-code-cloud"><div class="comment" id="c2"></div><button class="previous-conversation">Prev</button></div></div>`;
}

afterEach(() => {
  retireRepoDiffView();
  retireRepoDiffConversationNav();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('diff file-view toggle binds once, skips contribution-owned, and retire detaches', () => {
  installDiff();
  initRepoDiffView();
  initRepoDiffView();
  const toggle = document.querySelector('.page-content > .file-view-toggle');
  const ownedToggle = document.querySelector('[data-extension-panel] .file-view-toggle');
  toggle.click();
  expect(toggle.classList.contains('active')).toBe(true);
  ownedToggle.click();
  expect(ownedToggle.classList.contains('active')).toBe(false);
  retireRepoDiffView();
  toggle.classList.remove('active');
  toggle.click();
  expect(toggle.classList.contains('active')).toBe(false);
  // re-entry rebinds fresh nodes once
  installDiff();
  initRepoDiffView();
  document.querySelector('.page-content > .file-view-toggle').click();
  expect(document.querySelector('.page-content > .file-view-toggle').classList.contains('active')).toBe(true);
});

test('conversation nav cycles anchors once per click and retire detaches', () => {
  installConversationNav();
  initRepoDiffConversationNav();
  initRepoDiffConversationNav();
  const firstPrev = document.querySelector('.comment-code-cloud .previous-conversation');
  firstPrev.click();
  // wraps to last conversation anchor
  expect(window.location.href).toContain('#c2');
  retireRepoDiffConversationNav();
  window.location.href = 'http://localhost/';
  firstPrev.click();
  expect(window.location.href).not.toContain('#c');
  // re-entry works
  installConversationNav();
  initRepoDiffConversationNav();
  document.querySelector('.comment-code-cloud .previous-conversation').click();
  expect(window.location.href).toContain('#c2');
});

test('conversation submit wires reactions with a page-owned abort signal', async () => {
  const {POST} = await import('../modules/fetch.js');
  const {initCompReactionSelector} = await import('./comp/ReactionSelector.js');
  document.body.innerHTML = `<div class="page-content"><div id="diff-file-boxes"></div>
    <div class="conversation-holder"><form action="/conversations/1"><textarea>note</textarea></form></div>
    <div data-path="a"><button class="add-code-comment" data-side="left" data-idx="1">add</button></div></div>`;
  POST.mockResolvedValue({text: async () => '<div class="conversation-holder" data-path="a" data-side="left" data-idx="1"><div class="dropdown"></div></div>'});
  initRepoDiffView();
  document.querySelector('.conversation-holder form').dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  await vi.waitFor(() => expect(initCompReactionSelector).toHaveBeenCalled());
  const signal = initCompReactionSelector.mock.lastCall[1]?.signal;
  expect(signal).toBeInstanceOf(AbortSignal);
  expect(signal.aborted).toBe(false);
  retireRepoDiffView();
  expect(signal.aborted).toBe(true);
});

test('page seam drives diff-view and conversation-nav lifecycles', () => {
  installDiff();
  installConversationNav();
  document.body.innerHTML += `<div id="diff-file-boxes"></div><button class="file-view-toggle" data-toggle-selector="#view-a">T</button><div id="view-a"></div>`;
  registerPageFeature('test-b04-diff-view', {init: initRepoDiffView, retire: retireRepoDiffView});
  registerPageFeature('test-b04-conv-nav', {init: initRepoDiffConversationNav, retire: retireRepoDiffConversationNav});
  setPageConfig({});
  initPageFeatures();
  initPageFeatures();
  document.querySelector('.file-view-toggle').click();
  expect(document.querySelector('.file-view-toggle').classList.contains('active')).toBe(true);
  retirePageFeatures();
  const toggle = document.querySelector('.file-view-toggle');
  toggle.classList.remove('active');
  toggle.click();
  expect(toggle.classList.contains('active')).toBe(false);
});
