import {reactive} from 'vue';

let diffTreeStoreReactive;
export function diffTreeStore() {
  if (!diffTreeStoreReactive || diffTreeStoreReactive !== window.config.pageData.diffFileInfo) {
    diffTreeStoreReactive = reactive({
      fileTreeIsVisible: false, fileListIsVisible: false, isLoadingNewData: false, selectedItem: '',
      ...window.config.pageData.diffFileInfo,
    });
    window.config.pageData.diffFileInfo = diffTreeStoreReactive;
  }
  return diffTreeStoreReactive;
}

export function resetDiffTreeStore() {
  diffTreeStoreReactive = undefined;
}
