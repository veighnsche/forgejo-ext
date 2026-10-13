import {createApp} from 'vue';
import {parseIssueHref} from '../utils.js';
import {createTippy} from '../modules/tippy.js';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

const popups = new Map();

export function retireContextPopups() {
  for (const popup of popups.values()) {
    popup.tippy?.destroy();
    popup.view?.unmount();
    popup.el.remove();
  }
  popups.clear();
}

export function initContextPopups() {
  const refIssues = document.querySelectorAll('.ref-issue');
  attachRefIssueContextPopup(refIssues);
}

export async function attachRefIssueContextPopup(refIssues) {
  for (const refIssue of refIssues) {
    if (refIssue.classList.contains('ref-external-issue') || refIssue.closest(contributionRootSelector) || popups.has(refIssue)) continue;

    const {owner, repo, index} = parseIssueHref(refIssue.getAttribute('href'));
    if (!owner) continue;

    const el = document.createElement('div');
    refIssue.parentNode.insertBefore(el, refIssue.nextSibling);
    const popup = {el};
    popups.set(refIssue, popup);
    const generation = currentPageGeneration();

    const {default: ContextPopup} = await import(/* webpackChunkName: "context-popup" */'../components/ContextPopup.vue');
    if (popups.get(refIssue) !== popup || generation !== currentPageGeneration() || !refIssue.isConnected) continue;
    const view = createApp(ContextPopup);
    popup.view = view;

    try {
      view.mount(el);
    } catch (err) {
      console.error(err);
      el.textContent = 'ContextPopup failed to load';
    }

    popup.tippy = createTippy(refIssue, {
      content: el,
      placement: 'top-start',
      interactive: true,
      role: 'dialog',
      interactiveBorder: 5,
      onShow: () => {
        el.firstChild.dispatchEvent(new CustomEvent('ce-load-context-popup', {detail: {owner, repo, index}}));
      },
    });
  }
}
