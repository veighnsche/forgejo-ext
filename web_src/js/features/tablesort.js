import {contributionRootSelector, navigateNative} from '../native-lifecycle.js';

const attached = new Set();

function onSortClick(event) {
  const header = event.currentTarget;
  tableSort(header.getAttribute('data-sortt-asc'), header.getAttribute('data-sortt-desc'), header.getAttribute('data-sortt-default'));
}

export function initTableSort() {
  for (const header of document.querySelectorAll('th[data-sortt-asc]') || []) {
    if (attached.has(header) || header.closest(contributionRootSelector)) continue;
    header.addEventListener('click', onSortClick);
    attached.add(header);
  }
}

export function retireTableSort() {
  for (const header of attached) {
    header.removeEventListener('click', onSortClick);
    attached.delete(header);
  }
}

function tableSort(normSort, revSort, isDefault) {
  if (!normSort) return false;
  if (!revSort) revSort = '';

  const url = new URL(window.location);
  let urlSort = url.searchParams.get('sort');
  if (!urlSort && isDefault) urlSort = normSort;

  url.searchParams.set('sort', urlSort !== normSort ? normSort : revSort);
  navigateNative(url.href);
}
