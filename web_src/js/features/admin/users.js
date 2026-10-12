import {getPageData} from '../../native-lifecycle.js';

// Tracks attached listeners per form so retire removes exactly what init added.
const attached = new Map();

function onRadioClick(event) {
  event.currentTarget.form.submit();
}

function onResetClick(event) {
  event.preventDefault();
  // The reset control is an anchor (templates/admin/user/list.tmpl), which has
  // no form property, so resolve the owning form from the DOM.
  const form = event.currentTarget.closest('form');
  for (const input of form.querySelectorAll('input[type=radio]')) {
    if (input.name.startsWith('status_filter[')) {
      input.checked = false;
    }
  }
  form.submit();
}

export function initAdminUserListSearchForm() {
  const searchForm = getPageData().adminUserListSearchForm;
  if (!searchForm) return;

  const form = document.querySelector('#user-list-search-form');
  if (!form || attached.has(form)) return;

  for (const button of form.querySelectorAll(`button[name=sort][value="${searchForm.SortType}"]`)) {
    button.classList.add('active');
  }

  if (searchForm.StatusFilterMap) {
    for (const [k, v] of Object.entries(searchForm.StatusFilterMap)) {
      if (!v) continue;
      for (const input of form.querySelectorAll(`input[name="status_filter[${k}]"][value="${v}"]`)) {
        input.checked = true;
      }
    }
  }

  const listeners = [];
  for (const radio of form.querySelectorAll('input[type=radio]')) {
    radio.addEventListener('click', onRadioClick);
    listeners.push([radio, 'click', onRadioClick]);
  }

  for (const button of form.querySelectorAll('.j-reset-status-filter')) {
    button.addEventListener('click', onResetClick);
    listeners.push([button, 'click', onResetClick]);
  }
  attached.set(form, listeners);
}

export function retireAdminUserListSearchForm() {
  for (const [form, listeners] of attached) {
    for (const [el, type, handler] of listeners) {
      el.removeEventListener(type, handler);
    }
    attached.delete(form);
  }
}
