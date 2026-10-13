import $ from 'jquery';
import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {POST} from '../modules/fetch.js';
import {initRepoEditor, retireRepoEditor} from './repo-editor.js';
import {createCodeEditor, destroyCodeEditor} from './codeeditor.ts';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));
vi.mock('./codeeditor.ts', () => ({createCodeEditor: vi.fn(), destroyCodeEditor: vi.fn()}));
vi.mock('../markup/content.js', () => ({initMarkupContent: vi.fn()}));
vi.mock('./contextpopup.js', () => ({attachRefIssueContextPopup: vi.fn()}));
vi.mock('../modules/modal.ts', () => ({showModal: vi.fn()}));

function installEditor() {
  document.body.innerHTML = `<div class="page-content repository editor"><form class="repository edit form ui">
    <div class="switch" data-write="write" data-preview="preview"><div class="item active" data-tab="write">Write</div><div class="item" data-tab="preview" data-url="/preview" data-context="/ctx" data-branch-path="main" data-markup-mode="comment">Preview</div></div>
    <div class="tab active" data-tab="write"><textarea></textarea></div><div class="tab" data-tab="preview"></div>
    <input id="tree_path" value="a/b"><input id="file-name" value="file.md">
    <div class="breadcrumb"><span class="section"><a href="#">a</a></span></div>
    <input class="js-quick-pull-choice-option" type="radio" value="commit-to-new-branch" button_text="Commit">
    <div class="quick-pull-branch-name" style="display:none"><input></div>
    <button id="commit-button" type="button">Commit</button>
    <div class="repository editor"><textarea id="edit_area">hello</textarea></div>
    <input name="page_has_posted" value="true">
  </form></div>`;
  $.fn.areYouSure = vi.fn();
}

afterEach(() => {
  retireRepoEditor();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
  delete $.fn.areYouSure;
});

test('editor quick-pull and filename bind once and retire detaches', async () => {
  installEditor();
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  initRepoEditor();
  initRepoEditor();
  await Promise.resolve();
  await new Promise((resolve) => setTimeout(resolve, 0));
  const quick = document.querySelector('.js-quick-pull-choice-option');
  quick.checked = true;
  quick.dispatchEvent(new Event('change'));
  expect(document.getElementById('commit-button').textContent).toBe('Commit');
  const filename = document.getElementById('file-name');
  filename.value = 'newdir/newfile.md';
  filename.dispatchEvent(new Event('input'));
  expect(document.getElementById('tree_path').value).toContain('newfile.md');
  retireRepoEditor();
  document.getElementById('tree_path').value = 'reset';
  filename.value = 'other.md';
  filename.dispatchEvent(new Event('input'));
  expect(document.getElementById('tree_path').value).toBe('reset');
});

test('editor preview uses current context and late response after retire is dropped', async () => {
  installEditor();
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  let resolvePost;
  POST.mockReturnValueOnce(new Promise((resolve) => { resolvePost = resolve }));
  initRepoEditor();
  await new Promise((resolve) => setTimeout(resolve, 0));
  document.querySelector('.switch .item[data-tab="preview"]').click();
  await vi.waitFor(() => expect(POST).toHaveBeenCalledTimes(1));
  expect(POST.mock.lastCall[0]).toBe('/preview');
  retireRepoEditor();
  retirePageFeatures();
  resolvePost({text: async () => '<p>late</p>'});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.querySelector('.tab[data-tab="preview"]').textContent).not.toContain('late');
});

test('editor async view is destroyed on retire and late resolve never mounts', async () => {
  installEditor();
  let resolveEditor;
  createCodeEditor.mockReturnValueOnce(new Promise((resolve) => { resolveEditor = resolve }));
  initRepoEditor();
  retireRepoEditor();
  retirePageFeatures();
  const fakeView = {setValue: vi.fn()};
  resolveEditor(fakeView);
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(destroyCodeEditor).toHaveBeenCalledWith(fakeView);
  // fresh entry creates a new view
  installEditor();
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  initRepoEditor();
  await vi.waitFor(() => expect(createCodeEditor).toHaveBeenCalled());
});

test('tab switching does not persist after retire', () => {
  installEditor();
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  initRepoEditor();
  const previewTab = document.querySelector('.switch .item[data-tab="preview"]');
  previewTab.click();
  expect(previewTab.classList.contains('active')).toBe(true);
  retireRepoEditor();
  // old nodes stay in place for this check: retired tabs must not switch
  previewTab.classList.remove('active');
  document.querySelector('.switch .item[data-tab="write"]').classList.add('active');
  previewTab.click();
  expect(previewTab.classList.contains('active')).toBe(false);
  installEditor();
  initRepoEditor();
  const freshPreview = document.querySelector('.switch .item[data-tab="preview"]');
  const freshWrite = document.querySelector('.switch .item[data-tab="write"]');
  expect(freshPreview.classList.contains('active')).toBe(false);
  expect(freshWrite.classList.contains('active')).toBe(true);
});

test('contribution-owned editor form is never bound', async () => {
  document.body.innerHTML = `<div data-extension-panel><form class="repository edit form"><input id="file-name" value="x"><input class="js-quick-pull-choice-option"></form></div>`;
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  initRepoEditor();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(createCodeEditor).not.toHaveBeenCalled();
});

test('page seam drives editor init and retire', async () => {
  installEditor();
  createCodeEditor.mockResolvedValue({setValue: vi.fn()});
  registerPageFeature('test-b04-editor', {init: initRepoEditor, retire: retireRepoEditor});
  setPageConfig({});
  initPageFeatures();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(createCodeEditor).toHaveBeenCalledTimes(1);
  retirePageFeatures();
  expect(destroyCodeEditor).toHaveBeenCalledTimes(1);
});
