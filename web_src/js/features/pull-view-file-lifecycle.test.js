import {vi} from 'vitest';
import {setPageConfig} from '../native-lifecycle.js';
import {POST} from '../modules/fetch.js';
import {countAndUpdateViewedFiles, initExpandAndCollapseFilesButton, initViewedCheckboxListenerFor, retireViewedFiles} from './pull-view-file.js';

vi.mock('../modules/fetch.js', () => ({POST: vi.fn(async () => ({})), GET: vi.fn()}));

function installViewed() {
  setPageConfig({prReview: {numberOfViewedFiles: 0, numberOfFiles: 1}, diffFileInfo: {files: []}});
  document.body.innerHTML = `<div class="page-content"><div class="diff-file-header"><form class="viewed-file-form" data-link="/viewed"><input type="checkbox" name="a.js"></form></div>
    <div class="file-content" data-folded="false"></div><span id="viewed-files-summary"></span><span id="viewed-files-summary-label" data-text-changed-template="%[1]d of %[2]d"></span>
    <button id="expand-files-btn" type="button"></button><button id="collapse-files-btn" type="button"></button></div>`;
  const content = document.querySelector('.file-content');
  const fold = document.createElement('button');
  fold.className = 'fold-file';
  content.append(fold);
}

afterEach(() => {
  retireViewedFiles();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('viewed checkbox posts once per change and retire removes the listener', () => {
  installViewed();
  initViewedCheckboxListenerFor();
  initViewedCheckboxListenerFor();
  const checkbox = document.querySelector('.viewed-file-form input[type=checkbox]');
  checkbox.checked = true;
  checkbox.dispatchEvent(new Event('input'));
  expect(POST).toHaveBeenCalledTimes(1);
  countAndUpdateViewedFiles();
  retireViewedFiles();
  checkbox.checked = false;
  checkbox.dispatchEvent(new Event('input'));
  expect(POST).toHaveBeenCalledTimes(1);
  initViewedCheckboxListenerFor();
  checkbox.dispatchEvent(new Event('input'));
  expect(POST).toHaveBeenCalledTimes(2);
});

test('expand and collapse buttons bind once and retire cleanly', () => {
  installViewed();
  initExpandAndCollapseFilesButton();
  initExpandAndCollapseFilesButton();
  const box = document.querySelector('.file-content');
  document.getElementById('collapse-files-btn').click();
  expect(box.getAttribute('data-folded')).toBe('true');
  retireViewedFiles();
  document.getElementById('expand-files-btn').click();
  expect(box.getAttribute('data-folded')).toBe('true');
});
