import {createTippy} from '../modules/tippy.js';
import {toggleElem} from '../utils/dom.js';
import {contributionRootSelector} from '../native-lifecycle.js';

let ellipsisController = null;
let notesController = null;
const statusTooltips = new Map();

export function retireRepoEllipsisButton() {
  ellipsisController?.abort();
  ellipsisController = null;
}

export function retireCommitStatuses() {
  for (const instance of statusTooltips.values()) instance.destroy();
  statusTooltips.clear();
}

export function retireCommitNotes() {
  notesController?.abort();
  notesController = null;
}

export function initRepoEllipsisButton() {
  retireRepoEllipsisButton();
  ellipsisController = new AbortController();
  const {signal} = ellipsisController;
  for (const button of document.querySelectorAll('.js-toggle-commit-body')) {
    if (button.closest(contributionRootSelector)) continue;
    button.addEventListener('click', function (e) {
      e.preventDefault();
      const expanded = this.getAttribute('aria-expanded') === 'true';
      toggleElem(this.parentElement.querySelector('.commit-body'));
      this.setAttribute('aria-expanded', String(!expanded));
    }, {signal});
  }
}

export function initCommitStatuses() {
  for (const element of document.querySelectorAll('[data-tippy="commit-statuses"]')) {
    if (statusTooltips.has(element) || element.closest(contributionRootSelector)) continue;
    const top = document.querySelector('.repository.file.list') || document.querySelector('.repository.diff');

    const instance = createTippy(element, {
      content: element.nextElementSibling,
      placement: top ? 'top-start' : 'bottom-start',
      interactive: true,
      role: 'dialog',
      theme: 'box-with-header',
      interactiveBorder: element.closest('.forced-push') ? 0 : 20,
    });
    statusTooltips.set(element, instance);
  }
}

export function initCommitNotes() {
  retireCommitNotes();
  const area = document.getElementById('commit-notes-edit-area');
  if (!area || area.closest(contributionRootSelector)) return;
  notesController = new AbortController();
  const {signal} = notesController;
  document.getElementById('commit-notes-edit-button')?.addEventListener('click', () => {
    document.getElementById('commit-notes-display-area').classList.add('tw-hidden');
    document.getElementById('commit-notes-edit-area').classList.remove('tw-hidden');
  }, {signal});

  document.getElementById('commit-notes-add-button')?.addEventListener('click', () => {
    document.getElementById('commit-notes-edit-area').classList.remove('tw-hidden');
  }, {signal});

  document.getElementById('commit-notes-cancel-button')?.addEventListener('click', () => {
    document.getElementById('commit-notes-edit-form').reset();
    document.getElementById('commit-notes-display-area')?.classList.remove('tw-hidden');
    document.getElementById('commit-notes-edit-area').classList.add('tw-hidden');
  }, {signal});
}
