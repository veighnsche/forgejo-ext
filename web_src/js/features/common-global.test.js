import $ from 'jquery';
import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {enhanceNativeNodes, initGlobalButtons, initGlobalLinkActions, initGlobalShowModal, retireNativeNodes} from './common-global.js';
import {createDropzone} from './dropzone.js';

// The dirty-confirm vendor plugin needs a global jQuery at import time and is
// outside this test's scope (fresh-node enhancement + index wiring).
vi.mock('../vendor/jquery.are-you-sure.js', () => ({}));
// htmx's ESM bundle needs XPathEvaluator at import time, absent in happy-dom;
// its error listeners are outside this test's scope.
vi.mock('../htmx.js', () => ({}));
vi.mock('./dropzone.js', () => ({createDropzone: vi.fn(async (el) => {
  const dz = {destroy: vi.fn()};
  el.dropzone = dz;
  return dz;
})}));

afterEach(() => {
  document.body.innerHTML = '';
});

test('enhanceNativeNodes enhances dropdowns only inside the given root', () => {
  document.body.innerHTML = `<div id="swapped"><div class="ui dropdown" id="fresh"></div></div>
    <div class="ui dropdown" id="stale"></div>`;
  const root = document.querySelector('#swapped');
  const dropdown = vi.spyOn($.fn, 'dropdown');
  try {
    enhanceNativeNodes(root);
    expect(dropdown).toHaveBeenCalled();
    for (const context of dropdown.mock.contexts) {
      for (const el of context.get()) {
        expect(root.contains(el)).toBe(true);
      }
    }
  } finally {
    dropdown.mockRestore();
  }
});

test('index wires document-delegated handlers as single globals', () => {
  const index = readFileSync('web_src/js/index.js', 'utf8');
  for (const name of ['native-dropdowns', 'native-tooltips', 'native-modal-close', 'native-common-listeners', 'native-navbar-toggle', 'native-language-menu', 'native-clipboard', 'native-quick-submit', 'native-button-enter', 'native-global-buttons', 'native-link-actions']) {
    expect(index).toContain(`registerGlobalFeature('${name}'`);
  }
  for (const direct of ['initGlobalCommon();', 'initDropdowns();', 'initGlobalTooltips();', 'initModalClose();', 'initHeadNavbarContentToggle();', 'initFootLanguageMenu();', 'initGlobalButtons();', 'initGlobalLinkActions();', 'initGlobalCopyToClipboardListener();', 'initGlobalEnterQuickSubmit();', 'initGlobalButtonClickOnEnter();', 'initGlobalFormDirtyLeaveConfirm();', 'initGlobalDropzone();', 'initDisabledInputs(document);']) {
    expect(index).not.toContain(`\n  ${direct}`);
  }
  expect(index).toContain('initGlobalFeatures();');
  expect(index).toContain('enhanceNativeNodes(document);');
  expect(index).toContain(`registerPageFeature('native-node-enhancement'`);
  expect(index).toContain('setNativeSwapEnhancer(');
});

test('re-enhancing the same nodes binds nothing twice', () => {
  document.body.innerHTML = `<div class="ui dropdown" id="twice"></div>
    <div class="tabular menu" id="tabs-twice"><a class="item active" data-tab="a">A</a></div>`;
  const dropdown = vi.spyOn($.fn, 'dropdown');
  const tabItem = document.querySelector('#tabs-twice .item');
  const listen = vi.spyOn(tabItem, 'addEventListener');
  try {
    const boundElements = () => dropdown.mock.contexts.flatMap((ctx) => ctx.get());
    enhanceNativeNodes(document);
    const first = boundElements();
    expect(first).toContain(document.querySelector('#twice'));
    enhanceNativeNodes(document);
    expect(boundElements()).toEqual(first);
    expect(listen.mock.calls.filter((call) => call[0] === 'click')).toHaveLength(1);
  } finally {
    dropdown.mockRestore();
    listen.mockRestore();
  }
});

test('custom dropdowns stay feature-owned: never tracked or destroyed', () => {
  document.body.innerHTML = `<div class="page-content" id="custom-region">
      <div class="ui dropdown custom" id="custom-dd"></div>
      <div class="ui dropdown" id="plain-dd"></div>
    </div>`;
  const customDd = document.querySelector('#custom-dd');
  const plainDd = document.querySelector('#plain-dd');
  const dropdown = vi.spyOn($.fn, 'dropdown');
  try {
    enhanceNativeNodes(document);
    retireNativeNodes();
    const destroyed = [];
    for (const [i, call] of dropdown.mock.calls.entries()) {
      if (call[0] === 'destroy') destroyed.push(...dropdown.mock.contexts[i].get());
    }
    expect(destroyed).toContain(plainDd);
    expect(destroyed).not.toContain(customDd);
  } finally {
    dropdown.mockRestore();
  }
});

test.each(['data-extension-panels', 'data-extension-page', 'data-extension-panel'])('retire destroys native tooltips and preserves %s owners', (attribute) => {
  document.body.innerHTML = `<div class="page-content">
      <span id="tip-in"></span>
      <div ${attribute} id="retained-root"><span id="retained-tip"></span></div>
    </div>
    <span id="tip-out"></span>`;
  const tipIn = document.querySelector('#tip-in');
  const tipOut = document.querySelector('#tip-out');
  const destroyIn = vi.fn();
  const destroyOut = vi.fn();
  const destroyRetainedRoot = vi.fn();
  const destroyRetainedTip = vi.fn();
  tipIn._tippy = {destroy: destroyIn};
  tipOut._tippy = {destroy: destroyOut};
  document.querySelector('#retained-root')._tippy = {destroy: destroyRetainedRoot};
  document.querySelector('#retained-tip')._tippy = {destroy: destroyRetainedTip};
  retireNativeNodes();
  expect(destroyIn).toHaveBeenCalledTimes(1);
  expect(destroyOut).not.toHaveBeenCalled();
  expect(destroyRetainedRoot).not.toHaveBeenCalled();
  expect(destroyRetainedTip).not.toHaveBeenCalled();
});

test('retire destroys region instances, keeps live chrome, and prunes detached nodes', () => {
  document.body.innerHTML = `<div class="page-content" id="region">
      <div class="ui dropdown" id="slot-dd"></div>
    </div>
    <div class="ui dropdown" id="chrome-dd"></div>
    <div data-extension-panels id="home">
      <div class="ui dropdown" id="home-dd"></div>
    </div>`;
  const slotDd = document.querySelector('#slot-dd');
  const homeDd = document.querySelector('#home-dd');
  const dropdown = vi.spyOn($.fn, 'dropdown');
  try {
    enhanceNativeNodes(document);
    const firstEnhanced = dropdown.mock.contexts.flatMap((ctx) => ctx.get());
    expect(firstEnhanced).not.toContain(homeDd);
    retireNativeNodes();
    const destroyed = [];
    for (const [i, call] of dropdown.mock.calls.entries()) {
      if (call[0] === 'destroy') destroyed.push(...dropdown.mock.contexts[i].get());
    }
    expect(destroyed).toContain(slotDd);
    expect(destroyed).not.toContain(document.querySelector('#chrome-dd'));

    // The retired slot node re-enhances; live chrome stays tracked.
    dropdown.mockClear();
    enhanceNativeNodes(document);
    const enhanced = dropdown.mock.contexts.flatMap((ctx) => ctx.get());
    expect(enhanced).toContain(slotDd);
    expect(enhanced).not.toContain(document.querySelector('#chrome-dd'));

    // Detached nodes are pruned without destroy and re-enhance on return.
    const slot = document.querySelector('#region');
    slot.remove();
    dropdown.mockClear();
    retireNativeNodes();
    expect(dropdown.mock.calls.filter((call) => call[0] === 'destroy')).toHaveLength(0);
    document.body.append(slot);
    dropdown.mockClear();
    enhanceNativeNodes(document);
    const reEnhanced = dropdown.mock.contexts.flatMap((ctx) => ctx.get());
    expect(reEnhanced).toContain(slotDd);
  } finally {
    dropdown.mockRestore();
  }
});

test('show-modal is delegated once and survives trigger replacement', () => {
  document.body.innerHTML = `<div class="page-content"><button class="show-modal" id="open" data-modal="#dlg">open</button></div>
    <div id="dlg" class="ui modal"><span class="name"></span></div>`;
  $.fn.modal = vi.fn(function () { return this });
  const modal = $.fn.modal;
  try {
    initGlobalShowModal();
    initGlobalShowModal();
    initGlobalButtons();
    document.querySelector('#open').click();
    expect(modal.mock.calls.filter((call) => call[0] === 'show')).toHaveLength(1);
    // Transplant: the trigger node is replaced wholesale, with no rebinding.
    document.querySelector('.page-content').innerHTML = '<button class="show-modal" id="open2" data-modal="#dlg">open</button>';
    modal.mockClear();
    document.querySelector('#open2').click();
    expect(modal.mock.calls.filter((call) => call[0] === 'show')).toHaveLength(1);
  } finally {
    delete $.fn.modal;
  }
});

test('delete-button opens its modal once after repeated init', () => {
  document.body.innerHTML = `<div class="page-content"><button class="delete-button" id="del" data-modal-id="del-dlg" data-name="thing">del</button></div>
    <div id="del-dlg"><span class="name"></span></div>`;
  $.fn.modal = vi.fn(function () { return this });
  const modal = $.fn.modal;
  try {
    initGlobalLinkActions();
    initGlobalLinkActions();
    initGlobalLinkActions();
    document.querySelector('#del').click();
    expect(modal.mock.calls.filter((call) => call[0] === 'show')).toHaveLength(1);
    expect(document.querySelector('#del-dlg .name').textContent).toBe('thing');
  } finally {
    delete $.fn.modal;
  }
});

test('panel toggles act once per click after repeated init', () => {
  document.body.innerHTML = `<button class="show-panel toggle" data-panel="#panel">toggle</button><div id="panel" class="tw-hidden"></div>`;
  initGlobalButtons();
  initGlobalButtons();
  const panel = document.querySelector('#panel');
  document.querySelector('.show-panel').click();
  expect(panel.classList.contains('tw-hidden')).toBe(false);
  document.querySelector('.show-panel').click();
  expect(panel.classList.contains('tw-hidden')).toBe(true);
});

test('dirty-form guards bind fresh forms once and rebind after retire', () => {
  document.body.innerHTML = `<div class="page-content"><form id="f1"><input name="a"></form></div>
    <div data-extension-page><form id="f2"></form></div>`;
  $.fn.areYouSure = vi.fn();
  try {
    enhanceNativeNodes(document);
    expect($.fn.areYouSure).toHaveBeenCalledTimes(1);
    enhanceNativeNodes(document);
    expect($.fn.areYouSure).toHaveBeenCalledTimes(1);
    retireNativeNodes();
    enhanceNativeNodes(document);
    expect($.fn.areYouSure).toHaveBeenCalledTimes(2);
  } finally {
    delete $.fn.areYouSure;
  }
});

test('enhancement enables fresh disabled inputs but leaves contribution inputs alone', () => {
  document.body.innerHTML = `<div class="page-content"><input class="js-enable" id="native-in" disabled></div>
    <div data-extension-panel><input class="js-enable" id="owned-in" disabled></div>`;
  enhanceNativeNodes(document);
  expect(document.querySelector('#native-in').hasAttribute('disabled')).toBe(false);
  expect(document.querySelector('#owned-in').hasAttribute('disabled')).toBe(true);
});

test('dropzones initialize once per fresh node and are destroyed on retire', async () => {
  document.body.innerHTML = `<div class="page-content"><div class="dropzone" id="dz-in"></div></div>
    <div class="dropzone" id="dz-chrome"></div>`;
  createDropzone.mockClear();
  enhanceNativeNodes(document);
  await vi.waitFor(() => expect(createDropzone).toHaveBeenCalledTimes(2));
  enhanceNativeNodes(document);
  expect(createDropzone).toHaveBeenCalledTimes(2);
  retireNativeNodes();
  expect(document.querySelector('#dz-in').dropzone.destroy).toHaveBeenCalledTimes(1);
  expect(document.querySelector('#dz-chrome').dropzone.destroy).not.toHaveBeenCalled();
});
