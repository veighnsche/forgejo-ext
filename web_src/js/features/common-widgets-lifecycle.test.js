import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {initAutoFocusEnd} from './autofocus-end.js';
import {initCopyContent, retireCopyContent} from './copycontent.js';
import {initCommentContent, retireCommentContent} from '../markup/content.js';
import {POST} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));
vi.mock('clippie', () => ({clippie: vi.fn(async () => true)}));
vi.mock('../modules/tippy.js', () => ({showTemporaryTooltip: vi.fn(), createTippy: vi.fn(() => ({destroy: vi.fn()}))}));
vi.mock('../utils.js', async (importOriginal) => {
  const mod = await importOriginal();
  return {...mod, convertImage: vi.fn()};
});

afterEach(() => {
  retireCopyContent();
  retireCommentContent();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('autofocus-end focuses last native input and skips contribution-owned', () => {
  document.body.innerHTML = `<input class="js-autofocus-end" id="first" value="ab"><input class="js-autofocus-end" id="second" value="cd">
    <div data-extension-panel><input class="js-autofocus-end" id="owned" value="ef"></div>`;
  const second = document.getElementById('second');
  const focus = vi.spyOn(second, 'focus');
  const setRange = vi.spyOn(second, 'setSelectionRange');
  initAutoFocusEnd();
  expect(focus).toHaveBeenCalledOnce();
  expect(setRange).toHaveBeenCalledWith(2, 2);
  expect(document.getElementById('first') === document.activeElement).toBe(false);
});

test('copy-content copies current DOM text once and retire aborts late fetch', async () => {
  const {GET} = await import('../modules/fetch.js');
  const {clippie} = await import('clippie');
  document.body.innerHTML = `<button id="copy-content">copy</button><div class="file-view"><div class="lines-code">hello</div></div>`;
  initCopyContent();
  initCopyContent();
  document.getElementById('copy-content').click();
  await vi.waitFor(() => expect(clippie).toHaveBeenCalledTimes(1));
  expect(clippie.mock.lastCall[0]).toContain('hello');
  retireCopyContent();
  clippie.mockClear();
  document.getElementById('copy-content').click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(clippie).not.toHaveBeenCalled();
  // re-entry with fresh link uses current context
  GET.mockResolvedValue({headers: {get: () => 'text/plain'}, text: async () => 'fresh'});
  document.body.innerHTML = `<button id="copy-content" data-link="/fresh-content">copy</button>`;
  initCopyContent();
  document.getElementById('copy-content').click();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledWith('/fresh-content', expect.anything()));
});

test('comment tasklist binds once per checkbox and retire detaches', async () => {
  document.body.innerHTML = `<div><div class="markup" data-can-edit="true"><ul><li class="task-list-item"><input type="checkbox" data-source-position="2" disabled></li></ul></div>
    <div class="raw-content" style="display:none">- [ ] task</div><div class="edit-content-zone" data-update-url="/update" data-context="ctx" data-content-version="1"></div></div>`;
  POST.mockResolvedValue({status: 200, json: async () => ({contentVersion: '2'})});
  initCommentContent();
  initCommentContent();
  const checkbox = document.querySelector('input[type=checkbox]');
  expect(checkbox.disabled).toBe(false);
  checkbox.checked = true;
  checkbox.dispatchEvent(new Event('input'));
  await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
  retireCommentContent();
  POST.mockClear();
  checkbox.checked = false;
  checkbox.dispatchEvent(new Event('input'));
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(POST).not.toHaveBeenCalled();
});

test('page seam drives copy-content and comment-content lifecycles', async () => {
  const {clippie} = await import('clippie');
  document.body.innerHTML = `<button id="copy-content">copy</button><div class="file-view"><div class="lines-code">x</div></div>`;
  registerPageFeature('test-b03-copy', {init: initCopyContent, retire: retireCopyContent});
  registerPageFeature('test-b03-comment', {init: initCommentContent, retire: retireCommentContent});
  setPageConfig({});
  initPageFeatures();
  document.getElementById('copy-content').click();
  await vi.waitFor(() => expect(clippie).toHaveBeenCalledTimes(1));
  retirePageFeatures();
  clippie.mockClear();
  document.getElementById('copy-content').click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(clippie).not.toHaveBeenCalled();
});
