import {
  currentPageGeneration,
  getPageData,
  initGlobalFeatures,
  initPageFeatures,
  navigateNative,
  registerGlobalFeature,
  registerPageFeature,
  retirePageFeatures,
  setNativeNavigator,
  setPageConfig,
} from './native-lifecycle.js';

const savedPageData = window.config.pageData;

afterEach(() => {
  window.config.pageData = savedPageData;
});

test('page features init, retire, and re-init with refreshed config across a transition', () => {
  const seen = [];
  registerPageFeature('test-config-refresh-a', {
    init: () => seen.push(['a', getPageData().marker]),
    retire: () => seen.push(['retire-a']),
  });
  registerPageFeature('test-config-refresh-b', {
    init: () => seen.push(['b', getPageData().marker]),
    retire: () => seen.push(['retire-b']),
  });

  setPageConfig({marker: 'first'});
  initPageFeatures();
  // A second init without retire must not double-initialize.
  initPageFeatures();
  retirePageFeatures();
  setPageConfig({marker: 'second'});
  initPageFeatures();
  retirePageFeatures();

  expect(seen).toEqual([
    ['a', 'first'], ['b', 'first'],
    ['retire-b'], ['retire-a'],
    ['a', 'second'], ['b', 'second'],
    ['retire-b'], ['retire-a'],
  ]);
});

test('global features run exactly once per document', () => {
  let calls = 0;
  registerGlobalFeature('test-global-once', () => calls++);
  initGlobalFeatures();
  initGlobalFeatures();
  expect(calls).toBe(1);
});

test('duplicate registration is rejected', () => {
  registerPageFeature('test-dup', {init: () => {}});
  expect(() => registerPageFeature('test-dup', {init: () => {}})).toThrow('already registered');
  registerGlobalFeature('test-dup-global', () => {});
  expect(() => registerGlobalFeature('test-dup-global', () => {})).toThrow('already registered');
});

test('retire runs every active feature even when one fails, then rethrows', () => {
  const retired = [];
  registerPageFeature('test-retire-fail', {
    init: () => {},
    retire: () => { throw new Error('boom') },
  });
  registerPageFeature('test-retire-ok', {
    init: () => {},
    retire: () => retired.push(true),
  });
  initPageFeatures();
  expect(() => retirePageFeatures()).toThrow('boom');
  expect(retired).toEqual([true]);
});

test('retire bumps the page generation so async work can detect staleness', () => {
  let initGeneration = -1;
  registerPageFeature('test-generation', {
    init: () => { initGeneration = currentPageGeneration() },
  });
  initPageFeatures();
  const before = currentPageGeneration();
  expect(initGeneration).toBe(before);
  // The failing feature registered above still rethrows on retire; the
  // generation must bump regardless so in-flight async work goes stale.
  expect(() => retirePageFeatures()).toThrow('boom');
  expect(currentPageGeneration()).not.toBe(before);
});

test('native navigation goes through the plugged host navigator when set', () => {
  const seen = [];
  setNativeNavigator((url) => seen.push(url));
  try {
    navigateNative('/admin/orgs?sort=newest');
    expect(seen).toEqual(['/admin/orgs?sort=newest']);
  } finally {
    setNativeNavigator(null);
  }
});

test('native navigation falls back to a history-replacing load without a host', () => {
  window.history.replaceState(null, '', '/admin/orgs');
  navigateNative('/admin/orgs?sort=oldest');
  expect(window.location.pathname + window.location.search).toBe('/admin/orgs?sort=oldest');
  window.history.replaceState(null, '', '/');
});

test('navigator registration rejects non-functions', () => {
  expect(() => setNativeNavigator('nope')).toThrow('function or null');
});
