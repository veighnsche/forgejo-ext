import {vi} from 'vitest';
import {h, onMounted, onUnmounted} from 'vue';

const renderTree = () => h('div', 'Diff tree');
const renderSelect = () => h('div', 'Commit select');

function deferred() {
  let resolve;
  const promise = new Promise((accept) => { resolve = accept });
  return {promise, resolve};
}

beforeEach(() => {
  vi.resetModules();
  document.body.innerHTML = '';
});

afterEach(async () => {
  try { (await import('./repo-diff-filetree.js')).retireDiffFileTree() } catch {}
  try { (await import('./repo-diff-commitselect.js')).retireDiffCommitSelect() } catch {}
  document.body.innerHTML = '';
  vi.doUnmock('../components/DiffFileTree.vue');
  vi.doUnmock('../components/DiffCommitSelector.vue');
});

test('diff file tree mounts once and unmounts on retire', async () => {
  const events = [];
  vi.doMock('../components/DiffFileTree.vue', () => ({default: {
    setup() { onMounted(() => events.push('mount')); onUnmounted(() => events.push('retire')); return renderTree },
  }}));
  const {initDiffFileTree, retireDiffFileTree} = await import('./repo-diff-filetree.js');
  document.body.innerHTML = `<div id="diff-file-tree"></div>`;
  await initDiffFileTree();
  await initDiffFileTree();
  expect(events).toEqual(['mount']);
  retireDiffFileTree();
  expect(events).toEqual(['mount', 'retire']);
  // re-entry mounts fresh
  document.body.innerHTML = `<div id="diff-file-tree"></div>`;
  await initDiffFileTree();
  expect(events).toEqual(['mount', 'retire', 'mount']);
});

test('diff file tree drops late import after retire or detach', async () => {
  const loaded = deferred();
  const importer = vi.fn(() => loaded.promise);
  vi.doMock('../components/DiffFileTree.vue', importer);
  const {initDiffFileTree, retireDiffFileTree} = await import('./repo-diff-filetree.js');
  document.body.innerHTML = `<div id="diff-file-tree"></div>`;
  const pending = initDiffFileTree();
  await vi.waitFor(() => expect(importer).toHaveBeenCalledOnce());
  retireDiffFileTree();
  document.body.innerHTML = ``;
  loaded.resolve({default: {render: renderTree}});
  await pending;
  expect(document.body.textContent).toBe('');
});

test('diff commit select mounts once and unmounts on retire', async () => {
  const events = [];
  vi.doMock('../components/DiffCommitSelector.vue', () => ({default: {
    setup() { onMounted(() => events.push('mount')); onUnmounted(() => events.push('retire')); return renderSelect },
  }}));
  const {initDiffCommitSelect, retireDiffCommitSelect} = await import('./repo-diff-commitselect.js');
  document.body.innerHTML = `<div id="diff-commit-select"></div>`;
  await initDiffCommitSelect();
  await initDiffCommitSelect();
  expect(events).toEqual(['mount']);
  retireDiffCommitSelect();
  expect(events).toEqual(['mount', 'retire']);
});
