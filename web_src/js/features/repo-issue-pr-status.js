import {contributionRootSelector} from '../native-lifecycle.js';

// Element-bound toggles: tracked so retire removes exactly what init added
// and a repeated init never stacks a second handler on the same button.
const attached = new Set();

function onToggleClick(event) {
  const btn = event.currentTarget;
  const panel = btn.closest('.commit-status-panel');
  const list = panel.querySelector('.commit-status-list');
  list.style.maxHeight = list.style.maxHeight ? '' : '0px'; // toggle
  btn.textContent = btn.getAttribute(list.style.maxHeight ? 'data-show-all' : 'data-hide-all');
}

export function initRepoPullRequestCommitStatus() {
  for (const btn of document.querySelectorAll('.commit-status-hide-checks')) {
    if (attached.has(btn) || btn.closest(contributionRootSelector)) continue;
    btn.addEventListener('click', onToggleClick);
    attached.add(btn);
  }
}

export function retireRepoPullRequestCommitStatus() {
  for (const btn of attached) {
    btn.removeEventListener('click', onToggleClick);
    attached.delete(btn);
  }
}
