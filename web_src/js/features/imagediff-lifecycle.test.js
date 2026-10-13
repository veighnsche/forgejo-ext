import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {loadElem} from '../utils/dom.js';
import {initImageDiff, retireImageDiff} from './imagediff.js';

vi.mock('../utils/dom.js', async (importOriginal) => {
  const mod = await importOriginal();
  return {...mod, loadElem: vi.fn(async () => [true])};
});
vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

function installDiff() {
  document.body.innerHTML = `<div class="page-content"><div class="diff-file-box"><div class="image-diff" data-path-after="/a.png" data-mime-after="image/png" data-path-before="/b.png" data-mime-before="image/png">
    <img class="image-after"><img class="image-before">
    <div class="diff-swipe"><div class="swipe-container"></div><div class="swipe-bar"></div></div>
    <input type="range" value="50"><div class="image-diff-tabs is-loading"></div>
  </div></div></div>`;
  return document.querySelector('.image-diff');
}

afterEach(() => {
  retireImageDiff();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('image diff loads once per entry and retire releases handlers and flags', async () => {
  const container = installDiff();
  initImageDiff();
  initImageDiff();
  await vi.waitFor(() => expect(loadElem).toHaveBeenCalledTimes(2));
  await vi.waitFor(() => expect(container.querySelector('.image-diff-tabs').classList.contains('is-loading')).toBe(false));
  expect(container.getAttribute('data-image-diff-loaded')).toBe('true');
  // swipe is live: mousedown + mousemove moves the bar from its untouched state
  const bar = container.querySelector('.swipe-bar');
  bar.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, cancelable: true}));
  document.dispatchEvent(new MouseEvent('mousemove', {bubbles: true, cancelable: true, clientX: 10}));
  expect(bar.style.left).toBe('0px');
  document.dispatchEvent(new MouseEvent('mouseup', {bubbles: true}));
  retireImageDiff();
  expect(container.getAttribute('data-image-diff-loaded')).toBeNull();
  // retired swipe stays untouched by the same gesture
  bar.style.left = '';
  bar.dispatchEvent(new MouseEvent('mousedown', {bubbles: true, cancelable: true}));
  document.dispatchEvent(new MouseEvent('mousemove', {bubbles: true, cancelable: true, clientX: 10}));
  expect(bar.style.left).toBe('');
  // re-entry reloads the same container
  loadElem.mockClear();
  initImageDiff();
  await vi.waitFor(() => expect(loadElem).toHaveBeenCalledTimes(2));
});

test('retired range inputs no longer update overlay opacity', async () => {
  installDiff();
  initImageDiff();
  await vi.waitFor(() => expect(loadElem).toHaveBeenCalled());
  const range = document.querySelector('input[type="range"]');
  const afterParent = document.querySelector('img.image-after').parentNode;
  range.value = '80';
  range.dispatchEvent(new Event('input'));
  const updated = afterParent.style.opacity;
  retireImageDiff();
  range.value = '10';
  range.dispatchEvent(new Event('input'));
  expect(afterParent.style.opacity).toBe(updated);
});

test('contribution-owned image diffs are never loaded', async () => {
  document.body.innerHTML = `<div data-extension-panel><div class="image-diff" data-path-after="/a.png" data-mime-after="image/png"><img class="image-after"></div></div>`;
  initImageDiff();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(loadElem).not.toHaveBeenCalled();
  expect(document.querySelector('.image-diff').getAttribute('data-image-diff-loaded')).toBeNull();
});

test('page seam drives image-diff init and retire', async () => {
  installDiff();
  registerPageFeature('test-b04-image-diff', {init: initImageDiff, retire: retireImageDiff});
  setPageConfig({});
  initPageFeatures();
  await vi.waitFor(() => expect(loadElem).toHaveBeenCalled());
  retirePageFeatures();
  expect(document.querySelector('.image-diff').getAttribute('data-image-diff-loaded')).toBeNull();
});
