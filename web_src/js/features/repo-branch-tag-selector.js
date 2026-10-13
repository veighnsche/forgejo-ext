import {createApp} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

let branchMounts = [];

export function retireRepoBranchTagSelector() {
  const mounts = branchMounts;
  branchMounts = [];
  for (const mount of mounts) mount.app?.unmount();
}

export async function initRepoBranchTagSelector() {
  const roots = Array.from(document.querySelectorAll('.js-branch-tag-selector'))
    .filter((root) => root.closest('.page-content') && !root.closest(contributionRootSelector));
  if (!roots.length || roots.length === branchMounts.length && roots.every((root, index) => branchMounts[index].element === root)) return;

  retireRepoBranchTagSelector();
  const mounts = roots.map((element, index) => ({
    element,
    data: window.config.pageData.branchDropdownDataList?.[index],
    app: null,
  }));
  branchMounts = mounts;
  const generation = currentPageGeneration();
  try {
    const {default: RepoBranchTagSelector} = await import(/* webpackChunkName: "repo-branch-tag-selector" */'../components/RepoBranchTagSelector.vue');
    for (const mount of mounts) {
      if (branchMounts !== mounts || generation !== currentPageGeneration()) {
        if (branchMounts === mounts) retireRepoBranchTagSelector();
        return;
      }
      if (!mount.element.isConnected || !mount.element.closest('.page-content') || mount.element.closest(contributionRootSelector)) continue;
      mount.app = createApp({
        ...RepoBranchTagSelector,
        data() {
          return {
            ...RepoBranchTagSelector.data(),
            items: [],
            searchTerm: '',
            refNameText: '',
            menuVisible: false,
            release: null,
            isViewTag: false,
            isViewBranch: false,
            isViewTree: false,
            active: 0,
            isLoading: false,
            hasListInitialized: {branches: false, tags: false},
            ...mount.data,
          };
        },
      });
      mount.app.mount(mount.element);
    }
  } catch (error) {
    if (branchMounts !== mounts) return;
    retireRepoBranchTagSelector();
    throw error;
  }
}
