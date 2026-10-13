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
  readNativePageData,
} from './native-lifecycle.js';

const savedPageData = window.config.pageData;
const savedMentionValues = window.config.mentionValues;

afterEach(() => {
  window.config.pageData = savedPageData;
  window.config.mentionValues = savedMentionValues;
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
  setNativeNavigator((url, options) => seen.push([url, options]));
  try {
    navigateNative('/admin/orgs?sort=newest');
    navigateNative('/repo/compare?branch=main', {replace: false});
    expect(seen).toEqual([
      ['/admin/orgs?sort=newest', {replace: true}],
      ['/repo/compare?branch=main', {replace: false}],
    ]);
  } finally {
    setNativeNavigator(null);
  }
});

test('native template data joins the base context and each branch control without evaluating code', () => {
  const native = new DOMParser().parseFromString(`<body data-page-data='{"dashboardRepoList":{"uid":2,"searchLimit":10}}'>
    <script type="application/json" data-native-page-data="dashboardRepoList">{"textMyRepos":"Repositories"}</script>
    <script type="application/json" data-native-page-data="branchDropdownDataList" data-native-page-data-list>{"refName":"main"}</script>
    <script type="application/json" data-native-page-data="branchDropdownDataList" data-native-page-data-list>{"refName":"release"}</script>
    <div data-extension-page><script type="application/json" data-native-page-data="dashboardRepoList">{"uid":99}</script></div>
    <script>throw Error("Must not execute entered scripts")</script>
  </body>`, 'text/html');
  expect(readNativePageData(native)).toEqual({
    dashboardRepoList: {uid: 2, searchLimit: 10, textMyRepos: 'Repositories'},
    branchDropdownDataList: [{refName: 'main'}, {refName: 'release'}],
  });
});

test('native mentions replace the preceding page context and preserve deduplication', () => {
  const native = new DOMParser().parseFromString(`<body data-page-data='{}'>
    <script type="application/json" data-native-page-data="mentionValues">[{"name":"reader","value":"old"},{"name":"reader","value":"current"}]</script>
  </body>`, 'text/html');
  setPageConfig(readNativePageData(native));
  expect(window.config.mentionValues).toEqual([{name: 'reader', value: 'current'}]);
  setPageConfig({});
  expect(window.config.mentionValues).toEqual([]);
});

test('a malformed native configuration envelope is refused', () => {
  for (const body of [
    '<body>',
    '<body data-page-data="[]">',
    '<body data-page-data="{}"><script data-native-page-data="branchDropdownDataList">invalid</script>',
  ]) expect(readNativePageData(new DOMParser().parseFromString(body, 'text/html'))).toBeNull();
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
