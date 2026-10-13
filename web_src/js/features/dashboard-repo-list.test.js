import {vi} from 'vitest';
import {h, nextTick, onMounted, onUnmounted} from 'vue';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));
vi.mock('../svg.js', () => ({SvgIcon: {render: () => null}}));

const savedPageData = window.config.pageData;
let feature;

function installDashboard() {
  document.body.innerHTML = '<div class="page-content dashboard"><div id="dashboard-repo-list"></div></div>';
  return document.getElementById('dashboard-repo-list');
}

function deferred() {
  let resolve;
  const promise = new Promise((accept) => { resolve = accept });
  return {promise, resolve};
}

const renderProbe = () => h('div', {class: 'dashboard-probe'}, 'Dashboard');

function probeComponent(events) {
  return {
    setup() {
      onMounted(() => events.push('mounted'));
      onUnmounted(() => events.push('unmounted'));
      return renderProbe;
    },
  };
}

function dashboardConfig(uid) {
  return {
    dashboardRepoList: {
      uid, teamId: 0, searchLimit: 10, isOrganization: false, isMirrorsEnabled: false,
      ...Object.fromEntries([
        'textMyRepos', 'textMyOrgs', 'textSearchRepos', 'textFilter',
        'textShowArchived', 'textShowPrivate', 'textShowBothArchivedUnarchived',
        'textShowOnlyUnarchived', 'textShowOnlyArchived', 'textShowBothPrivatePublic',
        'textShowOnlyPublic', 'textShowOnlyPrivate', 'textAll', 'textSources',
        'textForks', 'textMirrors', 'textCollaborative', 'textFirstPage',
        'textPreviousPage', 'textNextPage', 'textLastPage',
      ].map((name) => [name, name])),
    },
  };
}

beforeEach(() => {
  vi.resetModules();
  vi.doUnmock('../components/DashboardRepoList.vue');
  GET.mockResolvedValue({headers: new Headers({'X-Total-Count': '0'}), json: async () => ({data: []})});
  window.history.replaceState(null, '', '/');
});

afterEach(() => {
  feature?.retireDashboardRepoList();
  feature = undefined;
  document.body.innerHTML = '';
  window.config.pageData = savedPageData;
});

test('the native page seam mounts once and unmounts before the next dashboard entry', async () => {
  const events = [];
  vi.doMock('../components/DashboardRepoList.vue', () => ({default: probeComponent(events)}));
  feature = await import('./dashboard-repo-list.ts');
  const {registerPageFeature, initPageFeatures, retirePageFeatures} = await import('../native-lifecycle.js');
  let initialized;
  registerPageFeature('dashboard-repo-list', {
    init: () => { initialized = feature.initDashboardRepoList() },
    retire: feature.retireDashboardRepoList,
  });
  const first = installDashboard();
  initPageFeatures();
  await initialized;
  initPageFeatures();
  await feature.initDashboardRepoList();
  expect(events).toEqual(['mounted']);
  expect(first.querySelector('.dashboard-probe')).not.toBeNull();
  retirePageFeatures();
  expect(events).toEqual(['mounted', 'unmounted']);
  expect(first.childNodes).toHaveLength(0);
  const second = installDashboard();
  initPageFeatures();
  await initialized;
  expect(second.querySelector('.dashboard-probe')).not.toBeNull();
  expect(events).toEqual(['mounted', 'unmounted', 'mounted']);
  retirePageFeatures();
  feature.retireDashboardRepoList();
  expect(events).toEqual(['mounted', 'unmounted', 'mounted', 'unmounted']);
});

test('a retired pending import cannot mount even when the same root is reused', async () => {
  const events = [];
  const loaded = deferred();
  const importer = vi.fn(() => loaded.promise);
  vi.doMock('../components/DashboardRepoList.vue', importer);
  feature = await import('./dashboard-repo-list.ts');
  const root = installDashboard();
  const stale = feature.initDashboardRepoList();
  await vi.waitFor(() => expect(importer).toHaveBeenCalledOnce());
  feature.retireDashboardRepoList();
  const current = feature.initDashboardRepoList();
  loaded.resolve({default: probeComponent(events)});
  await Promise.all([stale, current]);
  expect(events).toEqual(['mounted']);
  expect(root.querySelectorAll('.dashboard-probe')).toHaveLength(1);
});

test('a pending import cannot mount a detached or replaced native root', async () => {
  const events = [];
  const loaded = deferred();
  vi.doMock('../components/DashboardRepoList.vue', () => loaded.promise);
  feature = await import('./dashboard-repo-list.ts');
  const first = installDashboard();
  const pending = feature.initDashboardRepoList();
  const replacement = installDashboard();
  loaded.resolve({default: probeComponent(events)});
  await pending;
  expect(events).toEqual([]);
  expect(first.childNodes).toHaveLength(0);
  expect(replacement.childNodes).toHaveLength(0);
  await feature.initDashboardRepoList();
  expect(events).toEqual(['mounted']);
  expect(replacement.querySelector('.dashboard-probe')).not.toBeNull();
});

test('dashboard initialization leaves contribution-owned roots untouched', async () => {
  const events = [];
  vi.doMock('../components/DashboardRepoList.vue', () => ({default: probeComponent(events)}));
  feature = await import('./dashboard-repo-list.ts');
  document.body.innerHTML = '<div class="page-content"><div data-extension-panels><div id="dashboard-repo-list">Retained contribution</div></div></div>';
  await feature.initDashboardRepoList();
  expect(events).toEqual([]);
  expect(document.getElementById('dashboard-repo-list').textContent).toBe('Retained contribution');
});

test('the cached real dashboard component uses the new page configuration on re-entry', async () => {
  feature = await import('./dashboard-repo-list.ts');
  window.config.pageData = dashboardConfig(11);
  const first = installDashboard();
  await feature.initDashboardRepoList();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(2));
  await nextTick();
  expect(GET.mock.calls[0][0]).toContain('uid=11');
  const signal = GET.mock.calls[0][1].signal;
  feature.retireDashboardRepoList();
  expect(signal.aborted).toBe(true);
  expect(first.childNodes).toHaveLength(0);
  GET.mockClear();
  window.config.pageData = dashboardConfig(22);
  const second = installDashboard();
  await feature.initDashboardRepoList();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(2));
  expect(GET.mock.calls[0][0]).toContain('uid=22');
  expect(GET.mock.calls[1][0]).toContain('uid=22');
  expect(second.querySelector('input[type="search"]')).not.toBeNull();
});

test('retirement aborts real dashboard reads and prevents a late response from changing history', async () => {
  const response = deferred();
  GET.mockImplementation((_url, {signal}) => response.promise.then(() => {
    expect(signal.aborted).toBe(true);
    return {headers: new Headers({'X-Total-Count': '1'}), json: async () => ({data: []})};
  }));
  feature = await import('./dashboard-repo-list.ts');
  window.config.pageData = dashboardConfig(11);
  installDashboard();
  await feature.initDashboardRepoList();
  expect(GET).toHaveBeenCalledTimes(1);
  const signal = GET.mock.calls[0][1].signal;
  feature.retireDashboardRepoList();
  document.body.innerHTML = '<div class="page-content">New native page</div>';
  window.history.replaceState(null, '', '/issues?sort=newest');
  const history = vi.spyOn(window.history, 'replaceState');
  try {
    response.resolve();
    await vi.waitFor(() => expect(signal.aborted).toBe(true));
    await nextTick();
    expect(GET).toHaveBeenCalledTimes(1);
    expect(history).not.toHaveBeenCalled();
    expect(window.location.pathname + window.location.search).toBe('/issues?sort=newest');
  } finally {
    history.mockRestore();
  }
});

test('a late real dashboard response body cannot rewrite the next native page URL', async () => {
  const body = deferred();
  const json = vi.fn(() => body.promise);
  GET.mockResolvedValue({headers: new Headers({'X-Total-Count': '1'}), json});
  feature = await import('./dashboard-repo-list.ts');
  window.config.pageData = dashboardConfig(11);
  installDashboard();
  await feature.initDashboardRepoList();
  await vi.waitFor(() => expect(json).toHaveBeenCalledOnce());
  const signal = GET.mock.calls[1][1].signal;
  feature.retireDashboardRepoList();
  document.body.innerHTML = '<div class="page-content">New native page</div>';
  window.history.replaceState(null, '', '/issues?sort=newest');
  const history = vi.spyOn(window.history, 'replaceState');
  try {
    body.resolve({data: []});
    await body.promise;
    await nextTick();
    expect(signal.aborted).toBe(true);
    expect(history).not.toHaveBeenCalled();
    expect(window.location.pathname + window.location.search).toBe('/issues?sort=newest');
  } finally {
    history.mockRestore();
  }
});
