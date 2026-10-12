import $ from 'jquery';
import {showModal} from '../../modules/modal.ts';

function isExclusiveScopeName(name) {
  return /.*[^/]\/[^/].*/.test(name);
}

function updateExclusiveLabelEdit(form) {
  const nameInput = document.querySelector(`${form} .label-name-input`);
  const exclusiveField = document.querySelector(`${form} .label-exclusive-input-field`);
  const exclusiveCheckbox = document.querySelector(`${form} .label-exclusive-input`);
  const exclusiveWarning = document.querySelector(`${form} .label-exclusive-warning`);

  if (isExclusiveScopeName(nameInput.value)) {
    exclusiveField?.classList.remove('muted');
    exclusiveField?.removeAttribute('aria-disabled');
    exclusiveWarning?.classList.toggle('tw-hidden', !(exclusiveCheckbox.checked && exclusiveCheckbox.getAttribute('data-exclusive-warn')));
  } else {
    exclusiveField?.classList.add('muted');
    exclusiveField?.setAttribute('aria-disabled', 'true');
    exclusiveWarning?.classList.add('tw-hidden');
  }
}

function onNewLabelClick() {
  updateExclusiveLabelEdit('.new-label');
  showModal('new-label-modal', () => {
    const form = document.querySelector('.new-label.form');
    if (!form.checkValidity()) {
      form.reportValidity();
      return false;
    }
    document.querySelector('.new-label.form').requestSubmit();
  });
  return false;
}

function onEditLabelClick() {
  $('#label-modal-id').val($(this).data('id'));

  const $nameInput = $('.edit-label .label-name-input');
  $nameInput.val($(this).data('title'));

  const $isArchivedCheckbox = $('.edit-label .label-is-archived-input');
  $isArchivedCheckbox[0].checked = this.hasAttribute('data-is-archived');

  const $exclusiveCheckbox = $('.edit-label .label-exclusive-input');
  $exclusiveCheckbox[0].checked = this.hasAttribute('data-exclusive');
  // Warn when label was previously not exclusive and used in issues
  $exclusiveCheckbox.data('exclusive-warn',
    $(this).data('num-issues') > 0 &&
    (!this.hasAttribute('data-exclusive') || !isExclusiveScopeName($nameInput.val())));
  updateExclusiveLabelEdit('.edit-label');

  $('.edit-label .label-desc-input').val(this.getAttribute('data-description'));

  const colorInput = document.querySelector('.edit-label .js-color-picker-input input');
  colorInput.value = this.getAttribute('data-color');
  colorInput.dispatchEvent(new Event('input', {bubbles: true}));

  showModal('edit-label-modal', () => {
    const form = document.querySelector('.edit-label.form');
    if (!form.checkValidity()) {
      form.reportValidity();
      return false;
    }
    document.querySelector('.edit-label.form').requestSubmit();
  });
  return false;
}

function onNewLabelNameInput() {
  updateExclusiveLabelEdit('.new-label');
}

function onNewLabelExclusiveChange() {
  updateExclusiveLabelEdit('.new-label');
}

function onEditLabelNameInput() {
  updateExclusiveLabelEdit('.edit-label');
}

function onEditLabelExclusiveChange() {
  updateExclusiveLabelEdit('.edit-label');
}

// [selector, event, handler] bindings made by init, removed by retire.
const bindings = [
  ['.new-label.button', 'click', onNewLabelClick],
  ['.edit-label-button', 'click', onEditLabelClick],
  ['.new-label .label-name-input', 'input', onNewLabelNameInput],
  ['.new-label .label-exclusive-input', 'change', onNewLabelExclusiveChange],
  ['.edit-label .label-name-input', 'input', onEditLabelNameInput],
  ['.edit-label .label-exclusive-input', 'change', onEditLabelExclusiveChange],
];

// Tracks attached elements per binding so retire removes exactly what init
// added, even if the DOM is inspected again after a transition.
const attached = new Map();

export function initCompLabelEdit(selector) {
  if (!$(selector).length) return;

  for (const [sel, event, handler] of bindings) {
    let elements = attached.get(handler);
    if (!elements) {
      elements = new Set();
      attached.set(handler, elements);
    }
    for (const el of document.querySelectorAll(sel)) {
      if (elements.has(el)) continue;
      $(el).on(event, handler);
      elements.add(el);
    }
  }
}

export function retireCompLabelEdit() {
  for (const [, event, handler] of bindings) {
    const elements = attached.get(handler);
    if (!elements) continue;
    for (const el of elements) {
      $(el).off(event, handler);
      elements.delete(el);
    }
  }
}
