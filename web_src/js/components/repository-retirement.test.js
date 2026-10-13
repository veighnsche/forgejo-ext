import {vi} from 'vitest';
import {flushPromises, shallowMount} from '@vue/test-utils';
import {GET, POST} from '../modules/fetch.js';
import {Chart} from 'chart.js';
import {showErrorToast} from '../modules/toast.js';
import RepoActivityTopAuthors from './RepoActivityTopAuthors.vue';
import RepoContributors from './RepoContributors.vue';
import RepoCodeFrequency from './RepoCodeFrequency.vue';
import RepoRecentCommits from './RepoRecentCommits.vue';
import RepoActionView from './RepoActionView.vue';
import RepoBranchTagSelector from './RepoBranchTagSelector.vue';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn()}));
vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));
vi.mock('../svg.js', () => ({SvgIcon: {render: () => null}}));
vi.mock('chartjs-adapter-dayjs-4/dist/chartjs-adapter-dayjs-4.esm', () => ({}));
vi.mock('chartjs-plugin-zoom', () => ({default: {id: 'zoom'}}));
vi.mock('chart.js', () => ({
  Chart: {defaults: {}, register: vi.fn(), instances: {}},
  ...Object.fromEntries(['Tooltip', 'BarElement', 'CategoryScale', 'LinearScale', 'Legend', 'Title', 'TimeScale', 'PointElement', 'LineElement', 'Filler'].map((name) => [name, {}])),
}));
vi.mock('vue-chartjs', () => ({
  Bar: {props: ['data', 'options', 'plugins'], render: () => null},
  Line: {props: ['data', 'options', 'plugins'], render: () => null},
}));

const savedPageData = window.config.pageData;
const wrappers = [];
const graphLocale = {
  filterLabel: '', contributionType: {commits: '', additions: '', deletions: ''},
  loadingTitle: '', loadingTitleFailed: '', loadingInfo: '',
  codeFrequencyTitle: 'Frequency {0}', recentCommitsTitle: 'Recent commits',
};
const graphs = [
  {name: 'contributors', component: RepoContributors, field: 'totalStats', empty: {}, props: {repoLink: '/one/repository', repoDefaultBranchName: 'main'}},
  {name: 'code-frequency', component: RepoCodeFrequency, field: 'data', empty: [], props: {}},
  {name: 'recent-commits', component: RepoRecentCommits, field: 'data', empty: [], props: {}},
];

function deferred() {
  let resolve;
  const promise = new Promise((accept) => { resolve = accept });
  return {promise, resolve};
}

function graphWrapper(graph) {
  const wrapper = shallowMount(graph.component, {
    props: {locale: graphLocale, ...graph.props},
    global: {stubs: {'relative-time': true}},
  });
  wrappers.push(wrapper);
  return wrapper;
}

beforeEach(() => {
  window.config.pageData = {repoLink: '/one/repository', repoActivityTopAuthors: []};
});

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) wrapper.unmount();
  vi.useRealTimers();
  window.config.pageData = savedPageData;
});

for (const graph of graphs) {
  test(`${graph.name} aborts its read and cannot retry a retired 202 response`, async () => {
    vi.useFakeTimers();
    GET.mockResolvedValue({status: 202});
    const wrapper = graphWrapper(graph);
    await flushPromises();
    expect(GET).toHaveBeenCalledOnce();
    const signal = GET.mock.calls[0][1].signal;
    wrapper.unmount();
    expect(signal.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(2000);
    expect(GET).toHaveBeenCalledOnce();
  });

  test(`${graph.name} ignores a response body delivered after component retirement`, async () => {
    const body = deferred();
    const json = vi.fn(() => body.promise);
    GET.mockResolvedValue({status: 200, ok: true, json});
    const wrapper = graphWrapper(graph);
    await flushPromises();
    expect(json).toHaveBeenCalledOnce();
    const signal = GET.mock.calls[0][1].signal;
    const instance = wrapper.vm;
    wrapper.unmount();
    body.resolve({invalid: 'must not be parsed into retired UI'});
    await flushPromises();
    expect(signal.aborted).toBe(true);
    expect(instance[graph.field]).toEqual(graph.empty);
    expect(instance.errorText).toBe('');
  });
}

for (const graph of graphs.slice(1)) {
  test(`${graph.name} reads current page configuration when its cached component mounts again`, async () => {
    GET.mockResolvedValue({status: 403, ok: false, statusText: 'Denied'});
    const first = graphWrapper(graph);
    await flushPromises();
    expect(GET.mock.calls[0][0]).toContain('/one/repository/activity/');
    first.unmount();
    GET.mockClear();
    window.config.pageData = {repoLink: '/two/repository'};
    graphWrapper(graph);
    await flushPromises();
    expect(GET.mock.calls[0][0]).toContain('/two/repository/activity/');
  });
}

test('activity image-label plugins belong to individual charts and leave page configuration unchanged', async () => {
  const author = {name: 'First', commits: 1, avatar_link: '/first.png', home_link: '/first'};
  window.config.pageData = {repoActivityTopAuthors: [author]};
  const first = shallowMount(RepoActivityTopAuthors, {props: {locale: {commitActivity: 'Commits'}}});
  wrappers.push(first);
  const firstPlugin = first.vm.imageLabelPlugin;
  expect(firstPlugin.id).toBe('image_label');
  expect(author).not.toHaveProperty('avatar_img');
  window.config.pageData = {repoActivityTopAuthors: [{...author, name: 'Second'}]};
  const second = shallowMount(RepoActivityTopAuthors, {props: {locale: {commitActivity: 'Commits'}}});
  wrappers.push(second);
  expect(second.vm.imageLabelPlugin).not.toBe(firstPlugin);
  expect(first.vm.activityTopAuthors[0].name).toBe('First');
  expect(second.vm.activityTopAuthors[0].name).toBe('Second');
  expect(Chart.register).not.toHaveBeenCalledWith(expect.objectContaining({id: 'image_label'}));
});

const actionLocale = {
  ...Object.fromEntries(['approve', 'cancel', 'rerun', 'artifactsTitle', 'areYouSure', 'confirmDeleteArtifact', 'rerun_all', 'showTimeStamps', 'showLogSeconds', 'showFullScreen', 'downloadLogs', 'runAttemptLabel', 'viewingOutOfDateRun', 'viewMostRecentRun', 'preExecutionError'].map((name) => [name, ''])),
  status: {},
};
const job = {
  state: {
    run: {link: '/one/run', status: 'success', done: true, commit: {pusher: {}, branch: {}}},
    currentJob: {steps: [], details: [], allAttempts: []},
  },
  logs: {stepsLog: []},
};

function actionWrapper() {
  const wrapper = shallowMount(RepoActionView, {
    props: {
      initialJobData: job, initialArtifactData: {artifacts: []}, runIndex: '1', runID: '1',
      jobIndex: '1', attemptNumber: '1', actionsURL: '/one/actions',
      workflowName: 'Workflow', workflowURL: '/workflow', workflowSourceURL: '/workflow/source',
      locale: actionLocale,
    },
  });
  wrappers.push(wrapper);
  return wrapper;
}

test('a retired Actions mounted continuation cannot attach body or hash listeners', async () => {
  const body = vi.spyOn(document.body, 'addEventListener');
  const windowListener = vi.spyOn(window, 'addEventListener');
  try {
    const wrapper = actionWrapper();
    const instance = wrapper.vm;
    wrapper.unmount();
    await flushPromises();
    expect(body.mock.calls.filter(([type, listener]) => type === 'click' && listener === instance.closeDropdown)).toHaveLength(0);
    expect(windowListener.mock.calls.filter(([type, listener]) => type === 'hashchange' && listener === instance.hashChangeListener)).toHaveLength(0);
  } finally {
    body.mockRestore();
    windowListener.mockRestore();
  }
});

test('Actions aborts pending job/artifact reads and refuses late state or poll resurrection', async () => {
  const wrapper = actionWrapper();
  await flushPromises();
  const body = deferred();
  const json = vi.fn(() => body.promise);
  POST.mockResolvedValue({json});
  GET.mockResolvedValue({json: async () => ({artifacts: []})});
  const pending = wrapper.vm.loadJob();
  await flushPromises();
  expect(json).toHaveBeenCalledOnce();
  const jobSignal = POST.mock.calls[0][1].signal;
  const artifactSignal = GET.mock.calls[0][1].signal;
  const instance = wrapper.vm;
  wrapper.unmount();
  body.resolve({...job, state: {...job.state, run: {...job.state.run, link: '/late/run', done: false}}});
  await pending;
  expect(jobSignal.aborted).toBe(true);
  expect(artifactSignal.aborted).toBe(true);
  expect(instance.run.link).toBe('/one/run');
  expect(instance.intervalID).toBeNull();
  await instance.loadJob();
  expect(POST).toHaveBeenCalledOnce();
});

test('branch selector retirement aborts reads without late items or error toasts', async () => {
  const body = deferred();
  GET.mockResolvedValue({json: () => body.promise});
  const wrapper = shallowMount({
    ...RepoBranchTagSelector,
    data() {
      return {
        ...RepoBranchTagSelector.data(), items: [], searchTerm: '', refNameText: '', menuVisible: false,
        release: null, isViewTag: false, isViewBranch: false, isViewTree: false, isLoading: false,
        hasListInitialized: {branches: false, tags: false}, mode: 'branches', viewType: 'branch',
        repoLink: '/one/repository', branchName: 'main', showBranchesInDropdown: true,
        searchFieldPlaceholder: '', textBranches: '', textTags: '', noTag: false,
        disableCreateBranch: true, noResults: '',
      };
    },
  });
  wrappers.push(wrapper);
  const pending = wrapper.vm.fetchBranchesOrTags();
  await flushPromises();
  const instance = wrapper.vm;
  const signal = GET.mock.calls[0][1].signal;
  wrapper.unmount();
  body.resolve({results: ['late-branch']});
  await pending;
  expect(signal.aborted).toBe(true);
  expect(instance.items).toEqual([]);
  expect(showErrorToast).not.toHaveBeenCalled();
});
