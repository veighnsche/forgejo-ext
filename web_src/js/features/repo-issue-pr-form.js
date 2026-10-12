import {createApp} from 'vue';
import {contributionRootSelector} from '../native-lifecycle.js';

// The mounted Vue app is page-owned: retire unmounts it so a native
// transition never leaves a live app rendering into detached DOM.
let mountedView = null;

export async function initRepoPullRequestMergeForm() {
  const el = document.getElementById('pull-request-merge-form');
  if (!el || mountedView || el.closest(contributionRootSelector)) return;

  const {default: PullRequestMergeForm} = await import(/* webpackChunkName: "pull-request-merge-form" */'../components/PullRequestMergeForm.vue');
  // The element may have been transplanted away while the chunk loaded.
  if (!el.isConnected || mountedView) return;
  const view = createApp(PullRequestMergeForm);
  view.mount(el);
  mountedView = view;
}

export function retireRepoPullRequestMergeForm() {
  mountedView?.unmount();
  mountedView = null;
}
