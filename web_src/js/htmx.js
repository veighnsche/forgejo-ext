import htmx from 'htmx.org';
import {showErrorToast} from './modules/toast.js';
import {contributionRootSelector} from './native-lifecycle.js';

// https://github.com/bigskysoftware/idiomorph#htmx
import 'idiomorph/dist/idiomorph-ext.js';

// https://htmx.org/reference/#config
htmx.config.requestClass = 'is-loading';
htmx.config.scrollIntoViewOnBoost = false;
htmx.config.disableSelector = `${htmx.config.disableSelector},${contributionRootSelector}`;

const pendingNativeRequests = new Map();
let nativeRoot = null;

document.body.addEventListener('htmx:beforeSend', (event) => {
  const {elt, xhr} = event.detail;
  if (!(elt instanceof Element) || elt.closest(contributionRootSelector)) return;
  const root = elt.closest('.page-content');
  if (root && !root.closest(contributionRootSelector)) pendingNativeRequests.set(xhr, root);
});
document.body.addEventListener('htmx:afterRequest', (event) => {
  pendingNativeRequests.delete(event.detail.xhr);
});

export function initNativeHtmx() {
  nativeRoot = Array.from(document.querySelectorAll('.page-content')).find((root) => !root.closest(contributionRootSelector)) ?? null;
  if (nativeRoot) htmx.process(nativeRoot);
}

export function retireNativeHtmx() {
  const root = nativeRoot;
  nativeRoot = null;
  if (!root) return;
  const requests = [];
  for (const [xhr, owner] of pendingNativeRequests) {
    if (owner !== root) continue;
    pendingNativeRequests.delete(xhr);
    requests.push(xhr);
  }
  // A parks contribution owners before retiring native features. Public
  // innerHTML swap clears outgoing HTMX timers/listeners/request queues;
  // htmx.remove() only detaches DOM. Cleanup precedes abort so queued polls
  // cannot restart from xhr.onabort while their old source is connected.
  try {
    htmx.swap(root, '', {swapStyle: 'innerHTML', swapDelay: 0, settleDelay: 0, transition: false, ignoreTitle: true});
  } finally {
    for (const xhr of requests) xhr.abort();
  }
}

// https://htmx.org/events/#htmx:sendError
document.body.addEventListener('htmx:sendError', (event) => {
  // TODO: add translations
  showErrorToast(`Network error when calling ${event.detail.requestConfig.path}`);
});

// https://htmx.org/events/#htmx:responseError
document.body.addEventListener('htmx:responseError', (event) => {
  // hide any previous flash message to avoid confusions (in case the
  // error toast would have been shown over a success/info message)
  const flashMsgDiv = document.getElementById('flash-message');
  if (flashMsgDiv) {
    flashMsgDiv.innerHTML = '';
    flashMsgDiv.className = '';
  }
  // TODO: add translations
  showErrorToast(`Error ${event.detail.xhr.status} when calling ${event.detail.requestConfig.path}: ${event.detail.xhr.responseText}`);
});
