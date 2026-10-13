import {createApp, type App} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

type NativeMount = {element: HTMLElement; app?: App};

let nativeMount: NativeMount | null = null;

export function retireRepoActivityTopAuthorsChart() {
  const mount = nativeMount;
  nativeMount = null;
  mount?.app?.unmount();
}

export async function initRepoActivityTopAuthorsChart() {
  const el = document.getElementById('repo-activity-top-authors-chart');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || nativeMount?.element === el) return;

  retireRepoActivityTopAuthorsChart();
  const mount: NativeMount = {element: el};
  nativeMount = mount;
  const generation = currentPageGeneration();

  try {
    const {default: RepoActivityTopAuthors} = await import(/* webpackChunkName: "repo-activity-top-authors" */'../components/RepoActivityTopAuthors.vue');
    if (nativeMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('repo-activity-top-authors-chart') !== el) {
      if (nativeMount === mount) retireRepoActivityTopAuthorsChart();
      return;
    }
    mount.app = createApp(RepoActivityTopAuthors, {
      locale: {
        commitActivity: el.getAttribute('data-locale-commit-activity'),
      },
    });
    mount.app.mount(el);
  } catch (error) {
    if (nativeMount !== mount) return;
    retireRepoActivityTopAuthorsChart();
    throw error;
  }
}
