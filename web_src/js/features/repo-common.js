import $ from 'jquery';
import {hideElem, queryElems, showElem} from '../utils/dom.js';
import {POST} from '../modules/fetch.js';
import {showErrorToast} from '../modules/toast.js';
import {contributionRootSelector, navigateNative} from '../native-lifecycle.js';
import {toOriginUrl} from '../webcomponents/origin-url.js';

let archiveController = null;
let cloneController = null;
const branchDropdowns = new Set();
const filterDropdowns = new Set();

export function retireRepoCommon() {
  archiveController?.abort();
  archiveController = null;
  cloneController?.abort();
  cloneController = null;
  for (const el of branchDropdowns) $(el).find('.branch-tag-item').off('.repoCommon');
  for (const el of filterDropdowns) $(el).dropdown('destroy');
  branchDropdowns.clear();
  filterDropdowns.clear();
}

async function onDownloadArchive(e) {
  e.preventDefault();
  // there are many places using the "archive-link", eg: the dropdown on the repo code page, the release list
  const el = e.target.closest('a.archive-link[href]');
  const signal = archiveController?.signal;
  if (!signal) return;
  const targetLoading = el.closest('.ui.dropdown') ?? el;
  targetLoading.classList.add('is-loading', 'loading-icon-2px');
  try {
    for (let tryCount = 0; ;tryCount++) {
      const response = await POST(el.href, {signal});
      if (signal.aborted || !el.isConnected) return;
      if (!response.ok) throw new Error(`Invalid server response: ${response.status}`);

      const data = await response.json();
      if (signal.aborted || !el.isConnected) return;
      if (data.complete) break;
      await new Promise((resolve) => {
        let timer;
        const resume = () => {
          clearTimeout(timer);
          signal.removeEventListener('abort', resume);
          resolve();
        };
        timer = setTimeout(resume, Math.min((tryCount + 1) * 750, 2000));
        signal.addEventListener('abort', resume, {once: true});
      });
      if (signal.aborted || !el.isConnected) return;
    }
    window.location.href = el.href; // the archive is ready, start real downloading
  } catch (e) {
    if (signal.aborted) return;
    console.error(e);
    showErrorToast(`Failed to download the archive: ${e}`, {duration: 2500});
  } finally {
    targetLoading.classList.remove('is-loading', 'loading-icon-2px');
  }
}

export function initRepoArchiveLinks() {
  archiveController?.abort();
  archiveController = new AbortController();
  const {signal} = archiveController;
  queryElems('a.archive-link[href]', (el) => {
    if (!el.closest(contributionRootSelector)) el.addEventListener('click', onDownloadArchive, {signal});
  });
}

export function initRepoCloneLink() {
  cloneController?.abort();
  cloneController = new AbortController();
  const {signal} = cloneController;
  const repoCloneSSH = document.getElementById('repo-clone-ssh');
  const repoCloneHTTPS = document.getElementById('repo-clone-https');
  const inputLink = document.getElementById('repo-clone-url');
  updateCloneStates();

  if ((!repoCloneSSH && !repoCloneHTTPS) || !inputLink || inputLink.closest(contributionRootSelector)) {
    return;
  }

  repoCloneSSH?.addEventListener('click', () => {
    localStorage.setItem('repo-clone-protocol', 'ssh');
    updateCloneStates();
  }, {signal});
  repoCloneHTTPS?.addEventListener('click', () => {
    localStorage.setItem('repo-clone-protocol', 'https');
    updateCloneStates();
  }, {signal});

  inputLink.addEventListener('focus', () => {
    inputLink.select();
  }, {signal});
}

function updateCloneStates() {
  const httpsBtn = document.getElementById('repo-clone-https');
  const sshBtn = document.getElementById('repo-clone-ssh');
  if ((httpsBtn ?? sshBtn)?.closest(contributionRootSelector)) return;
  const value = localStorage.getItem('repo-clone-protocol') || 'https';
  const isSSH = value === 'ssh' && sshBtn || value !== 'ssh' && !httpsBtn;

  if (httpsBtn) {
    httpsBtn.textContent = window.origin.split(':')[0].toUpperCase();
    httpsBtn.classList.toggle('primary', !isSSH);
    httpsBtn.classList.toggle('basic', isSSH);
  }
  if (sshBtn) {
    sshBtn.classList.toggle('primary', isSSH);
    sshBtn.classList.toggle('basic', !isSSH);
  }

  const button = isSSH ? sshBtn : httpsBtn;
  if (!button) return;
  const link = toOriginUrl(button.getAttribute('data-link'));
  for (const el of document.getElementsByClassName('js-clone-url')) {
    if (!el.closest(contributionRootSelector)) el[el.nodeName === 'INPUT' ? 'value' : 'textContent'] = link;
  }
  for (const el of document.getElementsByClassName('js-clone-url-editor')) {
    if (!el.closest(contributionRootSelector)) el.href = el.getAttribute('data-href-template').replace('{url}', encodeURIComponent(link));
  }
}

export function initRepoCommonBranchOrTagDropdown(selector) {
  $(selector).each(function () {
    if (branchDropdowns.has(this) || this.closest(contributionRootSelector)) return;
    branchDropdowns.add(this);
    const $dropdown = $(this);
    $dropdown.find('.branch-tag-item').on('click.repoCommon', function () {
      hideElem($dropdown.find('.scrolling.reference-list-menu'));
      showElem($($(this).data('target')));
      return false;
    });
  });
}

export function initRepoCommonFilterSearchDropdown(selector) {
  $(selector).each((_, element) => {
    if (filterDropdowns.has(element) || element.closest(contributionRootSelector)) return;
    filterDropdowns.add(element);
    $(element).dropdown({
      fullTextSearch: 'exact',
      selectOnKeydown: false,
      onChange(_text, _value, $choice) {
        if (!filterDropdowns.has(element) || !element.isConnected) return;
        if ($choice[0].getAttribute('data-url')) {
          navigateNative($choice[0].getAttribute('data-url'), {replace: false});
        }
      },
      message: {noResults: element.getAttribute('data-no-results')},
    });
  });
}
