import $ from 'jquery';
import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {enhanceNativeNodes} from './common-global.js';

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
  for (const name of ['native-dropdowns', 'native-tooltips', 'native-modal-close', 'native-common-listeners']) {
    expect(index).toContain(`registerGlobalFeature('${name}'`);
  }
  for (const direct of ['initGlobalCommon();', 'initDropdowns();', 'initGlobalTooltips();', 'initModalClose();']) {
    expect(index).not.toContain(`\n  ${direct}`);
  }
  expect(index).toContain('initGlobalFeatures();');
  expect(index).toContain('enhanceNativeNodes(document);');
});
