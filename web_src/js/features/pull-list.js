// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

import {POST} from '../modules/fetch.js';
import {toggleElem} from '../utils/dom.js';

function preventNavigation(event) {
  event.preventDefault();
  event.returnValue = '';
}

// Never retry an ambiguous response: the server may still be completing a merge.
// Even when continuing after failures, an unknown result stops the entire batch.
export async function mergePullRequestsSequentially(pulls, options, onResult) {
  const results = [];
  let stopped = false;
  for (const pull of pulls) {
    let result;
    if (stopped) {
      result = {status: 'stopped'};
    } else {
      onResult(pull, {status: 'running'});
      try {
        const response = await POST(pull.url, {
          data: {
            do: options.mergeStyle,
            head_commit_id: pull.headCommitId,
            base_branch: pull.baseBranch,
            delete_branch_after_merge: options.deleteBranch,
          },
          redirect: 'error',
        });
        let body;
        try {
          body = await response.json();
        } catch {
          // Authentication, quota or proxy failures may not return JSON.
        }
        if (response.ok && body?.merged === true) {
          result = {status: 'merged', warning: body.warning};
        } else if (response.status >= 400 && response.status < 500 && response.status !== 408) {
          result = {status: 'skipped', message: typeof body?.message === 'string' ? body.message : ''};
        } else {
          result = {status: 'unknown'};
        }
      } catch {
        result = {status: 'unknown'};
      }
      stopped = result.status === 'unknown' || (result.status === 'skipped' && !options.continueOnFailure);
    }
    results.push(result);
    onResult(pull, result);
  }
  return results;
}

export function initPullRequestListMerge() {
  const button = document.querySelector('#pull-list-merge-button');
  const dialog = document.querySelector('#pull-list-merge-dialog');
  if (!button || !dialog) return;

  const issueList = document.querySelector('#issue-list');
  const clear = document.querySelector('#pull-list-merge-clear');
  const form = dialog.querySelector('form');
  const resultList = dialog.querySelector('.pull-list-merge-results');
  const summary = dialog.querySelector('#pull-list-merge-summary');
  const confirm = dialog.querySelector('.ok');
  const cancel = dialog.querySelector('.cancel');
  const reload = dialog.querySelector('.reload');
  const text = dialog.dataset;
  let pulls = [];
  let running = false;

  const selectedCheckboxes = () => Array.from(issueList.querySelectorAll('.issue-checkbox:checked:not(:disabled)'));
  const label = (count) => button.dataset.label.replace('%d', count);
  const sync = () => {
    const selected = selectedCheckboxes();
    const unavailable = selected.some((checkbox) => !checkbox.dataset.pullMergeUrl);
    button.querySelector('span').textContent = label(selected.length);
    button.disabled = running || selected.length === 0 || unavailable;
    button.title = unavailable ? button.dataset.unavailable : '';
    clear.disabled = running || selected.length === 0;
  };
  issueList.addEventListener('issue-selection-change', sync);
  sync();

  clear.addEventListener('click', () => {
    const selectAll = document.querySelector('.issue-checkbox-all');
    selectAll.checked = false;
    selectAll.dispatchEvent(new Event('change'));
  });

  button.addEventListener('click', () => {
    if (running || button.disabled) return;
    resultList.replaceChildren();
    pulls = selectedCheckboxes().map((checkbox) => {
      const sourceLink = checkbox.closest('.flex-item').querySelector('.issue-title');
      const item = document.createElement('li');
      const link = document.createElement('a');
      link.href = sourceLink.href;
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
      link.textContent = `${checkbox.dataset.pullLabel} ${sourceLink.textContent.trim()}`;
      const target = document.createElement('div');
      target.className = 'text light';
      target.textContent = checkbox.dataset.baseBranch;
      const status = document.createElement('p');
      status.textContent = text.waiting;
      status.className = 'text light';
      item.append(link, target, status);
      resultList.append(item);
      return {
        url: checkbox.dataset.pullMergeUrl,
        headCommitId: checkbox.dataset.headCommitId,
        baseBranch: checkbox.dataset.baseBranch,
        checkbox,
        status,
      };
    });
    summary.textContent = '';
    confirm.textContent = label(pulls.length);
    confirm.disabled = false;
    cancel.textContent = text.cancel;
    toggleElem(confirm, true);
    toggleElem(reload, false);
    document.body.append(dialog);
    dialog.showModal();
    cancel.focus();
  });

  form.addEventListener('submit', (event) => event.preventDefault());
  dialog.addEventListener('cancel', (event) => {
    if (running) event.preventDefault();
  });
  // Keep the global click-outside handler from dismissing an active merge.
  dialog.addEventListener('click', (event) => {
    if (running && event.target === dialog) event.stopPropagation();
  });
  cancel.addEventListener('click', () => {
    if (!running) dialog.close();
  });
  reload.addEventListener('click', () => window.location.reload());

  confirm.addEventListener('click', async () => {
    if (running || pulls.length === 0 || confirm.disabled) return;
    const options = {
      mergeStyle: form.elements.do.value,
      deleteBranch: form.elements.delete_branch_after_merge.checked,
      continueOnFailure: form.elements.continue_on_failure.checked,
    };
    running = true;
    confirm.disabled = true;
    cancel.disabled = true;
    for (const element of form.elements) element.disabled = true;
    window.addEventListener('beforeunload', preventNavigation);
    sync();
    try {
      const results = await mergePullRequestsSequentially(pulls, options, (pull, result) => {
        let message = text[result.status];
        if (result.status === 'skipped') message += `: ${result.message || text.requestFailed}`;
        if (result.warning) message += `: ${result.warning}`;
        pull.status.textContent = message;
        pull.status.className = result.status === 'merged' ? (result.warning ? 'text yellow' : 'text green') : 'text light';
        if (result.status === 'skipped' || result.status === 'unknown') pull.status.className = 'text red';
        if (result.status === 'running') {
          summary.textContent = text.running;
        } else if (result.status === 'merged' || result.status === 'unknown') {
          pull.checkbox.checked = false;
          pull.checkbox.disabled = true;
          pull.checkbox.dispatchEvent(new Event('change'));
        }
      });
      summary.textContent = text.summary
        .replace('%[1]d', results.filter((result) => result.status === 'merged').length)
        .replace('%[2]d', results.filter((result) => result.status === 'skipped').length)
        .replace('%[3]d', results.filter((result) => result.status === 'stopped').length)
        .replace('%[4]d', results.filter((result) => result.status === 'unknown').length);
    } finally {
      running = false;
      window.removeEventListener('beforeunload', preventNavigation);
      for (const element of form.elements) element.disabled = false;
      cancel.disabled = false;
      cancel.textContent = text.close;
      toggleElem(confirm, false);
      toggleElem(reload, true);
      sync();
    }
  });
}
