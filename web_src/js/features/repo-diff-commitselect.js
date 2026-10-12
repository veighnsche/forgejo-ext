import {createApp} from 'vue';

let commitSelectApp = null;
let commitSelectEl = null;

export async function initDiffCommitSelect() {
  const el = document.getElementById('diff-commit-select');
  if (!el) return;
  if (commitSelectApp && commitSelectEl === el) return;
  retireDiffCommitSelect();
  const {default: DiffCommitSelector} = await import(/* webpackChunkName: "diff-commit-selector" */'../components/DiffCommitSelector.vue');
  if (!el.isConnected || document.getElementById('diff-commit-select') !== el) return;
  commitSelectEl = el;
  commitSelectApp = createApp(DiffCommitSelector);
  commitSelectApp.mount(el);
}

export function retireDiffCommitSelect() {
  commitSelectApp?.unmount();
  commitSelectApp = null;
  commitSelectEl = null;
}
