import {contributionRootSelector} from '../../native-lifecycle.js';
import {showTemporaryTooltip} from '../../modules/tippy.js';
import {POST} from '../../modules/fetch.js';

const {appSubUrl} = window.config;

// Tracks attached checkboxes so retire removes exactly what init added.
const attached = new Set();

async function onConfigChange(event) {
  const el = event.currentTarget;
  try {
    const resp = await POST(`${appSubUrl}/admin/config`, {
      data: new URLSearchParams({key: el.getAttribute('data-config-dyn-key'), value: el.checked}),
    });
    const json = await resp.json();
    if (json.errorMessage) throw new Error(json.errorMessage);
  } catch (ex) {
    showTemporaryTooltip(el, ex.toString());
    el.checked = !el.checked;
  }
}

export function initAdminConfigs() {
  const elAdminConfig = document.querySelector('.page-content.admin.config');
  if (!elAdminConfig) return;

  for (const el of elAdminConfig.querySelectorAll('input[type="checkbox"][data-config-dyn-key]')) {
    if (attached.has(el) || el.closest(contributionRootSelector)) continue;
    el.addEventListener('change', onConfigChange);
    attached.add(el);
  }
}

export function retireAdminConfigs() {
  for (const el of attached) {
    el.removeEventListener('change', onConfigChange);
    attached.delete(el);
  }
}
