import {POST} from '../../modules/fetch.js';
import {toggleElem} from '../../utils/dom.js';

// Element-bound listeners owned by the current page; retire removes exactly
// what init added so repeated entry never stacks handlers.
let httpMethodInput = null;
let testDeliveryEl = null;

function onHttpMethodChange() {
  const visible = httpMethodInput.value === 'POST';
  toggleElem(document.getElementById('content_type').closest('.field'), visible);
}

async function onTestDeliveryClick() {
  this.classList.add('is-loading', 'disabled');
  await POST(this.getAttribute('data-link'));
  setTimeout(() => {
    window.location.href = this.getAttribute('data-redirect');
  }, 5000);
}

export function retireCompWebHookEditor() {
  if (httpMethodInput) {
    httpMethodInput.removeEventListener('change', onHttpMethodChange);
    httpMethodInput = null;
  }
  if (testDeliveryEl) {
    testDeliveryEl.removeEventListener('click', onTestDeliveryClick);
    testDeliveryEl = null;
  }
}

export function initCompWebHookEditor() {
  if (!document.querySelectorAll('.new.webhook').length) {
    return;
  }
  retireCompWebHookEditor();

  // some webhooks (like Gitea) allow to set the request method (GET/POST), and it would toggle the "Content Type" field
  httpMethodInput = document.getElementById('http_method');
  if (httpMethodInput) {
    onHttpMethodChange();
    httpMethodInput.addEventListener('change', onHttpMethodChange);
  }

  // Test delivery
  testDeliveryEl = document.getElementById('test-delivery');
  testDeliveryEl?.addEventListener('click', onTestDeliveryClick);
}
