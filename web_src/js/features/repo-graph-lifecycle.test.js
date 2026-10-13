import $ from 'jquery';
import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {GET} from '../modules/fetch.js';
import {initRepoGraphGit, retireRepoGraphGit} from './repo-graph.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

function installGraph() {
  document.body.innerHTML = `<div class="page-content"><div id="git-graph-container" class="colored"></div>
    <button id="flow-color-monochrome"></button><button id="flow-color-colored" class="active"></button>
    <div id="flow-select-refs-dropdown" class="ui dropdown"></div>
    <div class="pagination"><a href="/graph?x=1">p</a></div>
    <div id="pagination"></div><div id="rel-container"></div><div id="rev-container"></div><div id="loading-indicator" style="display:none"></div></div>`;
  window.history.replaceState({}, '', '/graph');
}

afterEach(() => {
  retireRepoGraphGit();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  window.history.replaceState({}, '', '/');
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

test('graph color toggle binds once per entry and retire detaches', () => {
  installGraph();
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    initRepoGraphGit();
    initRepoGraphGit();
    const container = document.getElementById('git-graph-container');
    document.getElementById('flow-color-monochrome').click();
    expect(container.classList.contains('monochrome')).toBe(true);
    expect(window.location.search).toContain('mode=monochrome');
    // dropdown initialized once per entry despite double init (set selected + options)
    const optionCalls = dropdown.mock.calls.filter((c) => typeof c[0] === 'object');
    expect(optionCalls).toHaveLength(1);
    retireRepoGraphGit();
    expect(dropdown).toHaveBeenLastCalledWith('destroy');
    container.classList.remove('monochrome');
    container.classList.add('colored');
    window.history.replaceState({}, '', '/graph');
    document.getElementById('flow-color-monochrome').click();
    expect(container.classList.contains('monochrome')).toBe(false);
    expect(window.location.search).not.toContain('mode=monochrome');
    // re-entry rebinds with fresh nodes
    installGraph();
    dropdown.mockClear();
    initRepoGraphGit();
    document.getElementById('flow-color-monochrome').click();
    expect(document.getElementById('git-graph-container').classList.contains('monochrome')).toBe(true);
  } finally {
    dropdown.mockRestore();
  }
});

test('graph branch selection uses current query and late response after retire is dropped', async () => {
  installGraph();
  window.history.replaceState({}, '', '/graph?branch=main');
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    GET.mockResolvedValue({text: async () => '<div><div id="pagination">P</div><div id="rel-container">R</div><div id="rev-container">V</div></div>'});
    initRepoGraphGit();
    const options = dropdown.mock.calls.find((c) => typeof c[0] === 'object')[0];
    options.onAdd('feature');
    await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(1));
    expect(GET.mock.lastCall[0]).toContain('branch=feature');
    expect(GET.mock.lastCall[0]).toContain('div-only=true');
    // late response after page retirement never renders
    let resolveLate;
    GET.mockReturnValueOnce(new Promise((resolve) => { resolveLate = resolve }));
    options.onAdd('late-branch');
    retireRepoGraphGit();
    retirePageFeatures();
    resolveLate({text: async () => '<div><div id="pagination">LATE</div><div id="rel-container">L</div><div id="rev-container">L</div></div>'});
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(document.getElementById('pagination').textContent).not.toBe('LATE');
  } finally {
    dropdown.mockRestore();
  }
});

test('contribution-owned graph container is never bound', () => {
  document.body.innerHTML = `<div data-extension-panel><div id="git-graph-container"></div><button id="flow-color-monochrome"></button><div id="flow-select-refs-dropdown"></div></div>`;
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    initRepoGraphGit();
    expect(dropdown).not.toHaveBeenCalled();
    document.getElementById('flow-color-monochrome').click();
  } finally {
    dropdown.mockRestore();
  }
});

test('page seam drives graph init and retire', () => {
  installGraph();
  const dropdown = vi.spyOn($.fn, 'dropdown').mockReturnThis();
  try {
    registerPageFeature('test-b04-graph', {init: initRepoGraphGit, retire: retireRepoGraphGit});
    setPageConfig({});
    initPageFeatures();
    document.getElementById('flow-color-monochrome').click();
    expect(document.getElementById('git-graph-container').classList.contains('monochrome')).toBe(true);
    retirePageFeatures();
    expect(dropdown).toHaveBeenLastCalledWith('destroy');
  } finally {
    dropdown.mockRestore();
  }
});
