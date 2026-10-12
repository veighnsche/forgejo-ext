import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {GET} from '../modules/fetch.js';
import {initFindFileInRepo, retireFindFileInRepo, filterRepoFilesWeighted} from './repo-findfile.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

function installFindFile() {
  document.body.innerHTML = `<div class="page-content"><input id="repo-file-find-input" data-url-tree-link="/o/r/src/branch/main" data-url-data-link="/o/r/files" value="">
    <table id="repo-find-file-table"><tbody></tbody></table><div id="repo-find-file-no-result"></div></div>`;
}

afterEach(() => {
  retireFindFileInRepo();
  retirePageFeatures();
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('find-file loads once, filters, and retire drops the listener', async () => {
  installFindFile();
  GET.mockResolvedValue({json: async () => ['src/main.js', 'README.md']});
  initFindFileInRepo();
  initFindFileInRepo();
  expect(GET).toHaveBeenCalledTimes(1);
  await vi.waitFor(() => expect(document.querySelectorAll('#repo-find-file-table tbody tr')).toHaveLength(2));
  const input = document.getElementById('repo-file-find-input');
  input.value = 'main';
  input.dispatchEvent(new Event('input'));
  expect(document.querySelectorAll('#repo-find-file-table tbody tr')).toHaveLength(1);
  retireFindFileInRepo();
  input.value = '';
  input.dispatchEvent(new Event('input'));
  expect(document.querySelectorAll('#repo-find-file-table tbody tr')).toHaveLength(1);
});

test('late file list after retire never renders', async () => {
  installFindFile();
  let resolveGet;
  GET.mockReturnValue(new Promise((resolve) => { resolveGet = resolve }));
  initFindFileInRepo();
  retirePageFeatures();
  resolveGet({json: async () => ['late.js']});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.querySelectorAll('#repo-find-file-table tbody tr')).toHaveLength(0);
});

test('contribution-owned find input is never bound', async () => {
  document.body.innerHTML = `<div data-extension-panel><input id="repo-file-find-input" data-url-tree-link="/x" data-url-data-link="/y"></div><table id="repo-find-file-table"><tbody></tbody></table><div id="repo-find-file-no-result"></div>`;
  GET.mockResolvedValue({json: async () => []});
  initFindFileInRepo();
  expect(GET).not.toHaveBeenCalled();
});

test('weighted filter stays pure across lifetimes', () => {
  expect(filterRepoFilesWeighted(['a/b.js', 'c.js'], 'b.js').length).toBeGreaterThan(0);
  retireFindFileInRepo();
  expect(filterRepoFilesWeighted(['a/b.js'], '')).toHaveLength(1);
});

test('page seam init and retire drive the find-file lifecycle', async () => {
  installFindFile();
  GET.mockResolvedValue({json: async () => ['one.js']});
  registerPageFeature('test-b04-findfile', {init: initFindFileInRepo, retire: retireFindFileInRepo});
  setPageConfig({});
  initPageFeatures();
  await vi.waitFor(() => expect(document.querySelectorAll('#repo-find-file-table tbody tr')).toHaveLength(1));
  retirePageFeatures();
  initPageFeatures();
  expect(GET).toHaveBeenCalledTimes(2);
});
