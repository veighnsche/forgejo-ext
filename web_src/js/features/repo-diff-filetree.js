import {createApp} from 'vue';

let fileTreeApp = null;
let fileTreeEl = null;

export async function initDiffFileTree() {
  const el = document.getElementById('diff-file-tree');
  if (!el) return;
  if (fileTreeApp && fileTreeEl === el) return;
  retireDiffFileTree();
  const {default: DiffFileTree} = await import(/* webpackChunkName: "diff-file-tree" */'../components/DiffFileTree.vue');
  // The page may have retired while the chunk loaded.
  if (!el.isConnected || document.getElementById('diff-file-tree') !== el) return;
  fileTreeEl = el;
  fileTreeApp = createApp(DiffFileTree);
  fileTreeApp.mount(el);
}

export function retireDiffFileTree() {
  fileTreeApp?.unmount();
  fileTreeApp = null;
  fileTreeEl = null;
}
