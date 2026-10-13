import {createApp, type App} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

type DashboardMount = {element: HTMLElement; app?: App};

let dashboardMount: DashboardMount | null = null;

export function retireDashboardRepoList() {
  const mount = dashboardMount;
  dashboardMount = null;
  mount?.app?.unmount();
}

export async function initDashboardRepoList() {
  const el = document.getElementById('dashboard-repo-list');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || dashboardMount?.element === el) {
    return;
  }

  retireDashboardRepoList();
  const mount: DashboardMount = {element: el};
  dashboardMount = mount;
  const generation = currentPageGeneration();
  try {
    const {default: DashboardRepoList} = await import(/* webpackChunkName: "dashboard-repo-list" */'../components/DashboardRepoList.vue');
    if (dashboardMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('dashboard-repo-list') !== el) {
      if (dashboardMount === mount) retireDashboardRepoList();
      return;
    }
    mount.app = createApp(DashboardRepoList);
    mount.app.mount(el);
  } catch (error) {
    if (dashboardMount === mount) retireDashboardRepoList();
    throw error;
  }
}
