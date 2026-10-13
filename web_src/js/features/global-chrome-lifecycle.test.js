import {vi} from 'vitest';
import {initDropdowns} from '../modules/dropdown.ts';
import {initGlobalCommonListeners, initHeadNavbarContentToggle, initFootLanguageMenu} from './common-global.js';
import {GET, request} from '../modules/fetch.js';

vi.mock('../vendor/jquery.are-you-sure.js', () => ({}));
vi.mock('../htmx.js', () => ({}));
vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn(), request: vi.fn()}));
vi.mock('../modules/tippy.js', () => ({createTippy: vi.fn(), showTemporaryTooltip: vi.fn()}));

afterEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

test('native details dropdowns move one item per keypress across repeated inits', () => {
  document.body.innerHTML = `<details class="dropdown" open><summary>Menu</summary>
    <div class="content"><ul><li><a href="#" id="i1">one</a></li><li><a href="#" id="i2">two</a></li><li><a href="#" id="i3">three</a></li></ul></div></details>`;
  initDropdowns();
  initDropdowns();
  document.getElementById('i2').focus();
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowUp', bubbles: true, cancelable: true}));
  // a single handler moves to the first item and stays open; stacked handlers
  // would keep going to the summary and close the dropdown
  expect(document.activeElement.id).toBe('i1');
  expect(document.querySelector('details.dropdown').hasAttribute('open')).toBe(true);
  // transplanted dropdowns keep working without rebinding
  document.body.innerHTML = `<details class="dropdown" open><summary>Menu</summary>
    <div class="content"><ul><li><a href="#" id="n1">one</a></li></ul></div></details><button id="outside">out</button>`;
  initDropdowns();
  document.getElementById('outside').click();
  expect(document.querySelector('details.dropdown').hasAttribute('open')).toBe(false);
});

test('global link actions post once per click across repeated inits', async () => {
  document.body.innerHTML = `<button class="link-action" data-url="/do-thing">go</button>`;
  request.mockResolvedValue({status: 200, json: async () => ({})});
  const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {});
  initGlobalCommonListeners();
  initGlobalCommonListeners();
  document.querySelector('.link-action').click();
  await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(1));
  await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
});

test('navbar toggle switches once per click across repeated inits', () => {
  document.body.innerHTML = `<nav id="navbar"></nav><button id="navbar-expand-toggle">menu</button>`;
  initHeadNavbarContentToggle();
  initHeadNavbarContentToggle();
  document.getElementById('navbar-expand-toggle').click();
  expect(document.getElementById('navbar').classList.contains('navbar-menu-open')).toBe(true);
  expect(document.getElementById('navbar-expand-toggle').classList.contains('active')).toBe(true);
});

test('language menu posts once per click across repeated inits', async () => {
  document.body.innerHTML = `<div class="language-menu"><a lang="fr" data-url="/lang/fr">FR</a></div>`;
  GET.mockResolvedValue({});
  const reload = vi.spyOn(window.location, 'reload').mockImplementation(() => {});
  initFootLanguageMenu();
  initFootLanguageMenu();
  document.querySelector('.language-menu a').click();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(1));
  expect(GET.mock.lastCall[0]).toBe('/lang/fr');
  await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
});

test('notification count polls on a single chain across repeated inits', async () => {
  vi.useFakeTimers();
  window.config.appSubUrl = '';
  window.config.notificationSettings = {MinTimeout: 10, MaxTimeout: 50, TimeoutStep: 10, EventSourceUpdateTime: 0};
  document.body.innerHTML = `<span class="notification_count">0</span>`;
  GET.mockResolvedValue({ok: true, json: async () => ({new: 0})});
  const {initNotificationCount} = await import('./notification.js');
  initNotificationCount();
  initNotificationCount();
  await vi.advanceTimersByTimeAsync(10);
  expect(GET).toHaveBeenCalledTimes(1);
});

test('stopwatch polls on a single chain and tips once across repeated inits', async () => {
  vi.useFakeTimers();
  window.config.appSubUrl = '';
  window.config.enableTimeTracking = true;
  window.config.notificationSettings = {MinTimeout: 10, MaxTimeout: 50, TimeoutStep: 10, EventSourceUpdateTime: 0};
  document.body.innerHTML = `<a class="active-stopwatch-trigger" href="/nojs">watch</a><div class="active-stopwatch-popup">popup</div>`;
  GET.mockResolvedValue({ok: true, json: async () => []});
  const {createTippy} = await import('../modules/tippy.js');
  const {initStopwatch} = await import('./stopwatch.js');
  initStopwatch();
  initStopwatch();
  expect(createTippy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.active-stopwatch-trigger').hasAttribute('href')).toBe(false);
  await vi.advanceTimersByTimeAsync(10);
  expect(GET).toHaveBeenCalledTimes(1);
});
