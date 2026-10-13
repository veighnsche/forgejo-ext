import {createApp} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

let nativeMount = null;

export function retireRepoCodeFrequency() {
  const mount = nativeMount;
  nativeMount = null;
  mount?.app?.unmount();
}

export async function initRepoCodeFrequency() {
  const el = document.getElementById('repo-code-frequency-chart');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || nativeMount?.element === el) return;

  retireRepoCodeFrequency();
  const mount = {element: el};
  nativeMount = mount;
  const generation = currentPageGeneration();

  try {
    const {default: RepoCodeFrequency} = await import(/* webpackChunkName: "code-frequency-graph" */'../components/RepoCodeFrequency.vue');
    if (nativeMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('repo-code-frequency-chart') !== el) {
      if (nativeMount === mount) retireRepoCodeFrequency();
      return;
    }
    mount.app = createApp(RepoCodeFrequency, {
      locale: {
        loadingTitle: el.getAttribute('data-locale-loading-title'),
        loadingTitleFailed: el.getAttribute('data-locale-loading-title-failed'),
        loadingInfo: el.getAttribute('data-locale-loading-info'),
        codeFrequencyTitle: el.getAttribute('data-locale-code-frequency-title'),
      },
    });
    mount.app.mount(el);
  } catch (err) {
    if (nativeMount !== mount) return;
    retireRepoCodeFrequency();
    console.error('RepoCodeFrequency failed to load', err);
    el.textContent = el.getAttribute('data-locale-component-failed-to-load');
  }
}
