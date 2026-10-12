import {diffTreeStore} from '../modules/stores.js';
import {setFileFolding} from './file-fold.js';
import {POST} from '../modules/fetch.js';
import {getPageData} from '../native-lifecycle.js';

const viewedStyleClass = 'viewed-file-checked-form';
const viewedCheckboxSelector = '.viewed-file-form';
const expandFilesBtnSelector = '#expand-files-btn';
const collapseFilesBtnSelector = '#collapse-files-btn';

function prReview() {
  return getPageData().prReview || {};
}

function refreshViewedFilesSummary() {
  const review = prReview();
  const viewedFilesProgress = document.getElementById('viewed-files-summary');
  viewedFilesProgress?.setAttribute('value', review.numberOfViewedFiles);
  const summaryLabel = document.getElementById('viewed-files-summary-label');
  if (summaryLabel) summaryLabel.innerHTML = summaryLabel.getAttribute('data-text-changed-template')
    .replace('%[1]d', review.numberOfViewedFiles)
    .replace('%[2]d', review.numberOfFiles);
}

export function countAndUpdateViewedFiles() {
  prReview().numberOfViewedFiles = document.querySelectorAll(`${viewedCheckboxSelector} > input[type=checkbox][checked]`).length;
  refreshViewedFilesSummary();
}

const checkboxHandlers = new Map();

function onViewedChange(form, checkbox) {
  return function () {
    const review = prReview();
    if (this.checked) {
      form.classList.add(viewedStyleClass);
      checkbox.setAttribute('checked', '');
      review.numberOfViewedFiles = (review.numberOfViewedFiles || 0) + 1;
    } else {
      form.classList.remove(viewedStyleClass);
      checkbox.removeAttribute('checked');
      review.numberOfViewedFiles = (review.numberOfViewedFiles || 0) - 1;
    }
    refreshViewedFilesSummary();
    form.parentNode?.querySelector('.changed-since-last-review')?.remove();
    const fileName = checkbox.getAttribute('name');
    const fileInPageData = diffTreeStore().files.find((x) => x.Name === fileName);
    if (fileInPageData) fileInPageData.IsViewed = this.checked;
    const files = {};
    files[fileName] = this.checked;
    const data = {files};
    const headCommitSHA = form.getAttribute('data-headcommit');
    if (headCommitSHA) data.headCommitSHA = headCommitSHA;
    POST(form.getAttribute('data-link'), {data});
    const parentBox = form.closest('.diff-file-header');
    const box = parentBox?.closest('.file-content');
    const fold = parentBox?.querySelector('.fold-file');
    if (box && fold) setFileFolding(box, fold, this.checked);
  };
}

export function initViewedCheckboxListenerFor() {
  for (const form of document.querySelectorAll(`${viewedCheckboxSelector}:not([data-has-viewed-checkbox-listener="true"])`)) {
    form.setAttribute('data-has-viewed-checkbox-listener', true);
    const checkbox = form.querySelector('input[type=checkbox]');
    if (!checkbox) continue;
    const handler = onViewedChange(form, checkbox);
    checkbox.addEventListener('input', handler);
    checkboxHandlers.set(checkbox, handler);
  }
}

let expandBtn = null;
let collapseBtn = null;

function onExpandFiles() {
  for (const box of document.querySelectorAll('.file-content[data-folded="true"]')) {
    const fold = box.querySelector('.fold-file');
    if (fold) setFileFolding(box, fold, false);
  }
}

function onCollapseFiles() {
  for (const box of document.querySelectorAll('.file-content:not([data-folded="true"])')) {
    if (box.getAttribute('id') === 'diff-incomplete') continue;
    const fold = box.querySelector('.fold-file');
    if (fold) setFileFolding(box, fold, true);
  }
}

export function initExpandAndCollapseFilesButton() {
  const nextExpand = document.querySelector(expandFilesBtnSelector);
  const nextCollapse = document.querySelector(collapseFilesBtnSelector);
  if (nextExpand && nextExpand !== expandBtn) {
    expandBtn?.removeEventListener('click', onExpandFiles);
    expandBtn = nextExpand;
    expandBtn.addEventListener('click', onExpandFiles);
  }
  if (nextCollapse && nextCollapse !== collapseBtn) {
    collapseBtn?.removeEventListener('click', onCollapseFiles);
    collapseBtn = nextCollapse;
    collapseBtn.addEventListener('click', onCollapseFiles);
  }
}

export function retireViewedFiles() {
  for (const [checkbox, handler] of checkboxHandlers) checkbox.removeEventListener('input', handler);
  checkboxHandlers.clear();
  for (const form of document.querySelectorAll(`${viewedCheckboxSelector}[data-has-viewed-checkbox-listener="true"]`)) {
    form.removeAttribute('data-has-viewed-checkbox-listener');
  }
  expandBtn?.removeEventListener('click', onExpandFiles);
  collapseBtn?.removeEventListener('click', onCollapseFiles);
  expandBtn = null;
  collapseBtn = null;
}
