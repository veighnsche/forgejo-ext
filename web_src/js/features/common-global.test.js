import $ from 'jquery';
import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {enhanceNativeNodes, retireNativeNodes} from './common-global.js';

// The dirty-confirm vendor plugin needs a global jQuery at import time and is
// outside this test's scope (fresh-node enhancement + index wiring).
vi.mock('../vendor/jquery.are-you-sure.js', () => ({}));
// htmx's ESM bundle needs XPathEvaluator at import time, absent in happy-dom;
// its error listeners are outside this test's scope.
vi.mock('../htmx.js', () => ({}));

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
  for (const name of ['native-dropdowns', 'native-tooltips', 'native-modal-close', 'native-common-listeners', 'native-navbar-toggle', 'native-language-menu']) {
    expect(index).toContain(`registerGlobalFeature('${name}'`);
  }
  for (const direct of ['initGlobalCommon();', 'initDropdowns();', 'initGlobalTooltips();', 'initModalClose();', 'initHeadNavbarContentToggle();', 'initFootLanguageMenu();']) {
    expect(index).not.toContain(`\n  ${direct}`);
  }
  expect(index).toContain('initGlobalFeatures();');
  expect(index).toContain('enhanceNativeNodes(document);');
  expect(index).toContain(`registerPageFeature('native-node-enhancement'`);
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

test('retire destroys slot instances, keeps live chrome, and prunes detached nodes', () => {
  document.body.innerHTML = `<div data-extension-workspace-direct-content id="slot">
      <div class="ui dropdown" id="slot-dd"></div>
    </div>
    <div class="ui dropdown" id="chrome-dd"></div>`;
  const slotDd = document.querySelector('#slot-dd');
  const dropdown = vi.spyOn($.fn, 'dropdown');
  try {
    enhanceNativeNodes(document);
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
    const slot = document.querySelector('#slot');
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
