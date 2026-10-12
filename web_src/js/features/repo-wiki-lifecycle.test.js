import {vi} from 'vitest';
import {initComboMarkdownEditor} from './comp/ComboMarkdownEditor.js';
import {initRepoWikiForm, retireRepoWikiForm} from './repo-wiki.js';

const screenListeners = new Set();
vi.mock('../modules/fomantic.js', () => ({
  fomanticMobileScreen: {
    matches: false,
    addEventListener: (_type, handler) => screenListeners.add(handler),
    removeEventListener: (_type, handler) => screenListeners.delete(handler),
  },
}));
vi.mock('./comp/ComboMarkdownEditor.js', () => ({
  validateTextareaNonEmpty: (textarea) => Boolean(textarea.value),
  initComboMarkdownEditor: vi.fn(async () => ({previewMode: 'gfm', previewContext: 'ctx', previewWiki: true, previewUrl: '/preview', easyMDE: null, container: document.querySelector('.combo-markdown-editor')})),
}));
vi.mock('../modules/fetch.js', () => ({POST: vi.fn(async () => ({text: async () => '<p>hi</p>'}))}));

function installWiki() {
  document.body.innerHTML = `<div class="page-content repository wiki"><div class="repository wiki new"><form class="ui form"><div class="combo-markdown-editor"><textarea>hello</textarea><div class="EasyMDEContainer"><div class="editor-preview-active"></div></div></div></form></div>
    <div class="wiki-content-toc"><details open></details></div></div>`;
}

afterEach(() => {
  retireRepoWikiForm();
  document.body.innerHTML = '';
  screenListeners.clear();
  vi.clearAllMocks();
  vi.useRealTimers();
});

test('wiki editor mounts once per entry and retire stops polling and listeners', async () => {
  vi.useFakeTimers();
  installWiki();
  initRepoWikiForm();
  initRepoWikiForm();
  expect(screenListeners.size).toBe(1);
  await vi.advanceTimersByTimeAsync(2500);
  expect(initComboMarkdownEditor).toHaveBeenCalledTimes(1);
  retireRepoWikiForm();
  expect(screenListeners.size).toBe(0);
  initRepoWikiForm();
  await vi.advanceTimersByTimeAsync(2500);
  expect(initComboMarkdownEditor).toHaveBeenCalledTimes(2);
  retireRepoWikiForm();
});

test('retired wiki form no longer validates on submit', async () => {
  vi.useFakeTimers();
  installWiki();
  initRepoWikiForm();
  await vi.advanceTimersByTimeAsync(2500);
  const form = document.querySelector('.repository.wiki.new .ui.form');
  const textarea = form.querySelector('textarea');
  textarea.value = '';
  const event = new Event('submit', {cancelable: true});
  form.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  retireRepoWikiForm();
  const event2 = new Event('submit', {cancelable: true});
  form.dispatchEvent(event2);
  expect(event2.defaultPrevented).toBe(false);
});
