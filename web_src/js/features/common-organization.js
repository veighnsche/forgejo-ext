import {initCompLabelEdit, retireCompLabelEdit} from './comp/LabelEdit.js';
import {toggleElem} from '../utils/dom.js';

// Tracks the attached input so retire removes exactly what init added.
const attached = new Set();

function onOrgNameInput(event) {
  const input = event.currentTarget;
  const nameChanged = input.value.toLowerCase() !== input.getAttribute('data-org-name').toLowerCase();
  toggleElem('#org-name-change-prompt', nameChanged);
}

export function initCommonOrganization() {
  if (!document.querySelectorAll('.organization').length) {
    return;
  }

  const orgName = document.querySelector('.organization.settings.options #org_name');
  if (orgName && !attached.has(orgName)) {
    orgName.addEventListener('input', onOrgNameInput);
    attached.add(orgName);
  }

  // Labels
  initCompLabelEdit('.organization.settings.labels');
}

export function retireCommonOrganization() {
  for (const el of attached) {
    el.removeEventListener('input', onOrgNameInput);
    attached.delete(el);
  }
  retireCompLabelEdit();
}
