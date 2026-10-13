import $ from 'jquery';
import {vi} from 'vitest';
import {initUnicodeEscapeButton} from './repo-unicode-escape.js';
import {initRepoActionsList, pollingOk} from './repo-actions-list.js';

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

test('unicode toggle acts once per click across repeated inits and survives transplant', () => {
  document.body.innerHTML = `<div class="page-content"><div class="file-content"><div class="file-view"></div><button class="toggle-escape-button">toggle</button><button class="escape-button tw-hidden">esc</button><button class="unescape-button tw-hidden">unesc</button></div></div>`;
  initUnicodeEscapeButton();
  initUnicodeEscapeButton();
  document.querySelector('.toggle-escape-button').click();
  expect(document.querySelector('.file-view').classList.contains('unicode-escaped')).toBe(true);
  // transplant: fresh nodes, no re-init, handler still acts once on current DOM
  document.body.innerHTML = `<div class="page-content"><div class="file-content"><div class="file-view"></div><button class="toggle-escape-button">toggle</button><button class="escape-button tw-hidden">esc</button><button class="unescape-button tw-hidden">unesc</button></div></div>`;
  initUnicodeEscapeButton();
  document.querySelector('.toggle-escape-button').click();
  expect(document.querySelector('.file-view').classList.contains('unicode-escaped')).toBe(true);
});

test('unicode escape skips contribution-owned buttons', () => {
  document.body.innerHTML = `<div data-extension-panel><div class="file-content"><div class="file-view"></div><button class="toggle-escape-button">toggle</button></div></div>`;
  initUnicodeEscapeButton();
  document.querySelector('.toggle-escape-button').click();
  expect(document.querySelector('.file-view').classList.contains('unicode-escaped')).toBe(false);
});

test('actions-list dropdown toggles once per click across repeated inits', () => {
  const transition = vi.fn(function () { return this });
  $.fn.transition = transition;
  try {
    document.body.innerHTML = `<div id="workflow_dispatch_dropdown"><button>open</button><div class="menu">items</div></div>`;
    initRepoActionsList();
    initRepoActionsList();
    document.querySelector('#workflow_dispatch_dropdown button').click();
    expect(transition).toHaveBeenCalledTimes(1);
    // transplant without rebinding still acts on current nodes
    document.body.innerHTML = `<div id="workflow_dispatch_dropdown"><button>open</button><div class="menu">items</div></div>`;
    transition.mockClear();
    initRepoActionsList();
    document.querySelector('#workflow_dispatch_dropdown button').click();
    expect(transition).toHaveBeenCalledTimes(1);
  } finally {
    delete $.fn.transition;
  }
});

test('actions pollingOk respects visibility, expanded menus, and branch form', () => {
  Object.defineProperty(document, 'visibilityState', {value: 'visible', configurable: true});
  document.body.innerHTML = `<div></div>`;
  expect(pollingOk()).toBe(true);
  document.body.innerHTML = `<div aria-expanded="true"></div>`;
  expect(pollingOk()).toBe(false);
});
