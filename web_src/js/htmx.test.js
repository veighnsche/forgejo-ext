import {vi} from 'vitest';
import htmx from 'htmx.org';
import {initNativeHtmx, retireNativeHtmx} from './htmx.js';
import {contributionRootSelector, initPageFeatures, registerPageFeature, retirePageFeatures} from './native-lifecycle.js';

vi.mock('htmx.org', () => ({default: {
  config: {disableSelector: '[hx-disable],[data-hx-disable]'},
  process: vi.fn(),
  swap: vi.fn(),
}}));
vi.mock('idiomorph/dist/idiomorph-ext.js', () => ({}));
vi.mock('./modules/toast.js', () => ({showErrorToast: vi.fn()}));

// Mocked-library host join checks. Actual pinned HTMX cleanup, polling and
// morph behavior remain part of the A03 real-browser proof.
beforeEach(() => {
  document.body.innerHTML = '<main class="page-content"><div id="poll" hx-get="/admin/system_status"></div></main><aside data-extension-panels><div data-extension-panel id="panel"></div></aside>';
  htmx.swap.mockImplementation((root, _content, spec) => {
    expect(spec).toEqual({swapStyle: 'innerHTML', swapDelay: 0, settleDelay: 0, transition: false, ignoreTitle: true});
    root.replaceChildren();
  });
});

afterEach(() => {
  retireNativeHtmx();
  document.body.innerHTML = '';
});

function signalRequest(elt, xhr, type = 'htmx:beforeSend') {
  elt.dispatchEvent(new CustomEvent(type, {bubbles: true, detail: {elt, xhr}}));
}

test('cleans outgoing queues before aborting pending native requests', () => {
  initNativeHtmx();
  const poll = document.getElementById('poll');
  const panel = document.getElementById('panel');
  let queued = true;
  const launchQueued = vi.fn();
  const xhr = {abort: vi.fn(() => {
    if (queued) launchQueued();
    expect(poll.isConnected).toBe(false);
    signalRequest(poll, xhr, 'htmx:afterRequest');
  })};
  signalRequest(poll, xhr);
  htmx.swap.mockImplementation((root) => {
    queued = false;
    root.replaceChildren();
  });
  retireNativeHtmx();
  expect(xhr.abort).toHaveBeenCalledOnce();
  expect(launchQueued).not.toHaveBeenCalled();
  expect(panel.isConnected).toBe(true);
  expect(document.getElementById('panel')).toBe(panel);
  retireNativeHtmx();
  expect(xhr.abort).toHaveBeenCalledOnce();
});

test('completed requests leave tracking before retirement', () => {
  initNativeHtmx();
  const poll = document.getElementById('poll');
  const xhr = {abort: vi.fn()};
  signalRequest(poll, xhr);
  signalRequest(poll, xhr, 'htmx:afterRequest');
  retireNativeHtmx();
  expect(xhr.abort).not.toHaveBeenCalled();
});

test('uses upstream disableSelector and excludes contribution requests', () => {
  expect(document.getElementById('panel').matches(htmx.config.disableSelector)).toBe(true);
  const page = document.createElement('div');
  page.setAttribute('data-extension-page', '');
  page.innerHTML = '<button id="private" hx-get="/private"></button>';
  document.querySelector('.page-content').append(page);
  const privateXHR = {abort: vi.fn()};
  signalRequest(document.getElementById('private'), privateXHR);
  const home = document.querySelector('[data-extension-panels]');
  home.append(page); // A parks the retained page before native retirement.
  initNativeHtmx();
  const root = document.querySelector('.page-content');
  expect(htmx.process).toHaveBeenCalledWith(root);
  expect(htmx.config.disableSelector).toContain(contributionRootSelector);
  retireNativeHtmx();
  expect(privateXHR.abort).not.toHaveBeenCalled();
  expect(page.isConnected).toBe(true);
  expect(home.contains(page)).toBe(true);
});

test('reverse page-feature retirement leaves native DOM available to other cleanup', () => {
  const controlRetire = vi.fn(() => expect(document.getElementById('poll')).not.toBeNull());
  registerPageFeature('test-native-htmx', {init: initNativeHtmx, retire: retireNativeHtmx});
  registerPageFeature('test-native-control', {init: () => {}, retire: controlRetire});
  initPageFeatures();
  retirePageFeatures();
  expect(controlRetire).toHaveBeenCalledOnce();
  expect(document.getElementById('poll')).toBeNull();
});
