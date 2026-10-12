import $ from 'jquery';
import {contributionRootSelector} from '../../native-lifecycle.js';
import {showModal} from '../../modules/modal.ts';

// Tracks attached actions so retire removes exactly what init added.
const attached = new Set();

function linkEmailAction(e) {
  const $this = $(this);
  $('#form-uid').val($this.data('uid'));
  $('#form-email').val($this.data('email'));
  $('#form-primary').val($this.data('primary'));
  $('#form-activate').val($this.data('activate'));
  showModal('change-email-modal', undefined);
  e.preventDefault();
}

export function initAdminEmails() {
  for (const el of document.querySelectorAll('.link-email-action')) {
    if (attached.has(el) || el.closest(contributionRootSelector)) continue;
    $(el).on('click', linkEmailAction);
    attached.add(el);
  }
}

export function retireAdminEmails() {
  for (const el of attached) {
    $(el).off('click', linkEmailAction);
    attached.delete(el);
  }
}
