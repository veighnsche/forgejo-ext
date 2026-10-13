import {vi} from 'vitest';
import {initPageFeatures, registerPageFeature, retirePageFeatures, setPageConfig} from '../native-lifecycle.js';
import {GET} from '../modules/fetch.js';
import {initRepoDiffCommitBranchesAndTags, retireRepoDiffCommitBranchesAndTags} from './repo-diff-commit.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

function installArea(url = '/branches-tags-1') {
  document.body.innerHTML = `<div class="page-content"><div class="branch-and-tag-area" data-text-default-branch-tooltip="default">
    <button class="load-branches-and-tags" data-fetch-url="${url}">Load</button>
    <div><div class="tag-area"></div></div><div><div class="branch-area"></div></div>
    <div class="branch-and-tag-detail" style="display:none"></div></div></div>`;
  return document.querySelector('.load-branches-and-tags');
}

afterEach(() => {
  retireRepoDiffCommitBranchesAndTags();
  try { retirePageFeatures() } catch {}
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

test('diff commit branches/tags load once, use current context, and retire detaches', async () => {
  const btn = installArea('/ctx-1');
  GET.mockResolvedValue({json: async () => ({tags: [{web_link: '/t/v1', name: 'v1'}], branches: [{web_link: '/b/main', name: 'main'}], default_branch: 'main'})});
  initRepoDiffCommitBranchesAndTags();
  initRepoDiffCommitBranchesAndTags();
  btn.click();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(1));
  expect(GET.mock.lastCall[0]).toBe('/ctx-1');
  await vi.waitFor(() => expect(document.querySelectorAll('.branch-area a')).toHaveLength(1));
  expect(document.querySelectorAll('.tag-area a')).toHaveLength(1);
  // repeated click on same loaded area must not duplicate links
  btn.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.querySelectorAll('.branch-area a')).toHaveLength(1);
  expect(document.querySelectorAll('.tag-area a')).toHaveLength(1);
  retireRepoDiffCommitBranchesAndTags();
  GET.mockClear();
  btn.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(GET).not.toHaveBeenCalled();
});

test('second entry reads fresh fetch URL and late first response never renders', async () => {
  let resolveFirst;
  GET.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }));
  const first = installArea('/stale-url');
  initRepoDiffCommitBranchesAndTags();
  first.click();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(1));
  retireRepoDiffCommitBranchesAndTags();
  retirePageFeatures();
  const second = installArea('/fresh-url');
  GET.mockResolvedValue({json: async () => ({tags: [], branches: [{web_link: '/b/fresh', name: 'fresh'}], default_branch: 'fresh'})});
  initRepoDiffCommitBranchesAndTags();
  second.click();
  await vi.waitFor(() => expect(document.querySelectorAll('.branch-area a')).toHaveLength(1));
  expect(GET.mock.lastCall[0]).toBe('/fresh-url');
  expect(document.querySelector('.branch-area a').textContent).toBe('fresh');
  resolveFirst({json: async () => ({tags: [{web_link: '/t/stale', name: 'stale'}], branches: [], default_branch: ''})});
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.querySelectorAll('.tag-area a')).toHaveLength(0);
});

test('contribution-owned areas are never bound', async () => {
  document.body.innerHTML = `<div data-extension-panel><div class="branch-and-tag-area"><button class="load-branches-and-tags" data-fetch-url="/owned">Load</button><div><div class="tag-area"></div></div><div><div class="branch-area"></div></div></div></div>`;
  GET.mockResolvedValue({json: async () => ({tags: [], branches: [], default_branch: ''})});
  initRepoDiffCommitBranchesAndTags();
  document.querySelector('.load-branches-and-tags').click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(GET).not.toHaveBeenCalled();
});

test('page seam drives diff-commit init and retire', async () => {
  const btn = installArea();
  GET.mockResolvedValue({json: async () => ({tags: [], branches: [], default_branch: ''})});
  registerPageFeature('test-b04-diff-commit', {init: initRepoDiffCommitBranchesAndTags, retire: retireRepoDiffCommitBranchesAndTags});
  setPageConfig({});
  initPageFeatures();
  btn.click();
  await vi.waitFor(() => expect(GET).toHaveBeenCalledTimes(1));
  retirePageFeatures();
  GET.mockClear();
  btn.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(GET).not.toHaveBeenCalled();
});
