import $ from 'jquery';
import {vi} from 'vitest';
import {getComboMarkdownEditor, initComboMarkdownEditor} from './ComboMarkdownEditor.js';
import {initRepoReleaseNew, retireRepoReleaseNew} from '../repo-release.js';
import {initRepoMilestoneEditor, retireRepoMilestoneEditor} from '../repo-milestone.js';
import {POST} from '../../modules/fetch.js';
import {renderPreviewPanelContent} from '../repo-editor.js';

const lazyEditor = vi.hoisted(() => {
  let resolve;
  const promise = new Promise((accept) => { resolve = accept });
  return {promise, resolve};
});
vi.mock('easymde', () => lazyEditor.promise);
vi.mock('../../modules/fetch.js', () => ({POST: vi.fn()}));
vi.mock('../repo-editor.js', () => ({renderPreviewPanelContent: vi.fn()}));
vi.mock('../../modules/toast.js', () => ({showErrorToast: vi.fn(), showHintToast: vi.fn()}));

function editorMarkup() {
  return `<div class="combo-markdown-editor">
    <markdown-toolbar><div class="switch"><button class="item active" data-tab-for="markdown-writer">Write</button><button class="item" data-tab-for="markdown-previewer" data-preview-url="/preview">Preview</button></div>
      <button class="markdown-switch-monospace" data-enable-text="Monospace" data-disable-text="Normal"></button></markdown-toolbar>
    <div class="ui tab active" data-tab-panel="markdown-writer"><text-expander><textarea class="markdown-text-editor">Draft</textarea></text-expander></div>
    <div class="ui tab" data-tab-panel="markdown-previewer">Loading</div>
    <div data-modal-name="new-markdown-table"><button data-selector-name="ok-button">Table</button></div>
    <div data-modal-name="new-markdown-link"><div data-selector-name="form"><input name="link-description"></div><button data-selector-name="ok-button">Link</button></div>
  </div>`;
}

beforeEach(() => {
  $.fn.modal = vi.fn().mockReturnThis();
  localStorage.removeItem('markdown-editor-default');
  localStorage.removeItem('markdown-editor-monospace');
});

afterEach(() => {
  retireRepoReleaseNew();
  retireRepoMilestoneEditor();
  for (const container of document.querySelectorAll('.combo-markdown-editor')) getComboMarkdownEditor(container)?.destroy();
  document.body.innerHTML = '';
  localStorage.removeItem('markdown-editor-default');
  localStorage.removeItem('markdown-editor-monospace');
  vi.restoreAllMocks();
});

test('native markdown entry is idempotent and retirement releases input/tab bindings', async () => {
  document.body.innerHTML = editorMarkup();
  const container = document.querySelector('.combo-markdown-editor');
  const changed = vi.fn();
  const editor = await initComboMarkdownEditor(container, {onContentChanged: changed});
  expect(await initComboMarkdownEditor(container)).toBe(editor);
  const textarea = container.querySelector('textarea');
  textarea.dispatchEvent(new Event('input'));
  expect(changed).toHaveBeenCalledOnce();
  editor.destroy();
  textarea.dispatchEvent(new Event('input'));
  expect(changed).toHaveBeenCalledOnce();
  expect(getComboMarkdownEditor(container)).toBeUndefined();
  expect(await initComboMarkdownEditor(container)).not.toBe(editor);
});

test('the native release and milestone callers release their real markdown editor on page retirement', async () => {
  document.body.innerHTML = `<div class="page-content repository new release">${editorMarkup()}</div>`;
  await initRepoReleaseNew();
  const release = document.querySelector('.combo-markdown-editor');
  expect(getComboMarkdownEditor(release)).toBeDefined();
  retireRepoReleaseNew();
  expect(getComboMarkdownEditor(release)).toBeUndefined();
  document.body.innerHTML = `<div class="page-content repository milestone">${editorMarkup()}</div>`;
  await initRepoMilestoneEditor();
  const milestone = document.querySelector('.combo-markdown-editor');
  expect(getComboMarkdownEditor(milestone)).toBeDefined();
  retireRepoMilestoneEditor();
  expect(getComboMarkdownEditor(milestone)).toBeUndefined();
});

test('each native markdown instance applies the saved monospace preference to its own textarea', async () => {
  localStorage.setItem('markdown-editor-monospace', 'true');
  document.body.innerHTML = editorMarkup() + editorMarkup();
  for (const container of document.querySelectorAll('.combo-markdown-editor')) await initComboMarkdownEditor(container);
  for (const textarea of document.querySelectorAll('textarea')) expect(textarea.classList.contains('tw-font-mono')).toBe(true);
});

test('a retired preview response cannot render into the outgoing native editor', async () => {
  let resolve;
  POST.mockReturnValue(new Promise((accept) => { resolve = accept }));
  document.body.innerHTML = editorMarkup();
  const container = document.querySelector('.combo-markdown-editor');
  const editor = await initComboMarkdownEditor(container);
  container.querySelector('[data-tab-for="markdown-previewer"]').click();
  const signal = POST.mock.lastCall[1].signal;
  editor.destroy();
  resolve({text: async () => '<p>Late preview</p>'});
  await Promise.resolve();
  await Promise.resolve();
  expect(signal.aborted).toBe(true);
  expect(renderPreviewPanelContent).not.toHaveBeenCalled();
});

test('retiring a pending legacy-editor import prevents creating EasyMDE on the outgoing root', async () => {
  localStorage.setItem('markdown-editor-default', 'easymde');
  document.body.innerHTML = editorMarkup();
  const container = document.querySelector('.combo-markdown-editor');
  const pending = initComboMarkdownEditor(container);
  getComboMarkdownEditor(container).destroy();
  const create = vi.fn();
  lazyEditor.resolve({default: create});
  await pending;
  expect(create).not.toHaveBeenCalled();
  expect(getComboMarkdownEditor(container)).toBeUndefined();
});
