import {createApp} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

let nativeMount = null;

export function retireRepoRecentCommits() {
  const mount = nativeMount;
  nativeMount = null;
  mount?.app?.unmount();
}

export async function initRepoRecentCommits() {
  const el = document.getElementById('repo-recent-commits-chart');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || nativeMount?.element === el) return;

  retireRepoRecentCommits();
  const mount = {element: el};
  nativeMount = mount;
  const generation = currentPageGeneration();

  try {
    const {default: RepoRecentCommits} = await import(/* webpackChunkName: "recent-commits-graph" */'../components/RepoRecentCommits.vue');
    if (nativeMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('repo-recent-commits-chart') !== el) {
      if (nativeMount === mount) retireRepoRecentCommits();
      return;
    }
    mount.app = createApp(RepoRecentCommits, {
      locale: {
        loadingTitle: el.getAttribute('data-locale-loading-title'),
        loadingTitleFailed: el.getAttribute('data-locale-loading-title-failed'),
        recentCommitsTitle: el.getAttribute('data-locale-recent-commits-title'),
        loadingInfo: el.getAttribute('data-locale-loading-info'),
      },
    });
    mount.app.mount(el);
  } catch (err) {
    if (nativeMount !== mount) return;
    retireRepoRecentCommits();
    console.error('RepoRecentCommits failed to load', err);
    el.textContent = el.getAttribute('data-locale-component-failed-to-load');
  }
}
