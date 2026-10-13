import {createApp} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

let nativeMount = null;

export function retireRepoContributors() {
  const mount = nativeMount;
  nativeMount = null;
  mount?.app?.unmount();
}

export async function initRepoContributors() {
  const el = document.getElementById('repo-contributors-chart');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || nativeMount?.element === el) return;

  retireRepoContributors();
  const mount = {element: el};
  nativeMount = mount;
  const generation = currentPageGeneration();

  try {
    const {default: RepoContributors} = await import(/* webpackChunkName: "contributors-graph" */'../components/RepoContributors.vue');
    if (nativeMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('repo-contributors-chart') !== el) {
      if (nativeMount === mount) retireRepoContributors();
      return;
    }
    mount.app = createApp(RepoContributors, {
      repoLink: el.getAttribute('data-repo-link'),
      repoDefaultBranchName: el.getAttribute('data-repo-default-branch-name'),
      locale: {
        filterLabel: el.getAttribute('data-locale-filter-label'),
        contributionType: {
          commits: el.getAttribute('data-locale-contribution-type-commits'),
          additions: el.getAttribute('data-locale-contribution-type-additions'),
          deletions: el.getAttribute('data-locale-contribution-type-deletions'),
        },

        loadingTitle: el.getAttribute('data-locale-loading-title'),
        loadingTitleFailed: el.getAttribute('data-locale-loading-title-failed'),
        loadingInfo: el.getAttribute('data-locale-loading-info'),
      },
    });
    mount.app.mount(el);
  } catch (err) {
    if (nativeMount !== mount) return;
    retireRepoContributors();
    console.error('RepoContributors failed to load', err);
    el.textContent = el.getAttribute('data-locale-component-failed-to-load');
  }
}
