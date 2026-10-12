import {svg} from '../svg.js';
import {invertFileFolding} from './file-fold.js';
import {createTippy} from '../modules/tippy.js';
import {clippie} from 'clippie';
import {toAbsoluteUrl} from '../utils.js';
import {contributionRootSelector} from '../native-lifecycle.js';

export const singleAnchorRegex = /^#[Ln]([1-9][0-9]*)$/;
export const rangeAnchorRegex = /^#[Ln]([1-9][0-9]*)-[Ln]?([1-9][0-9]*)$/;

function changeHash(hash) {
  if (window.history.pushState) {
    window.history.pushState(null, null, hash);
  } else {
    window.location.hash = hash;
  }
}

function isBlame() {
  return Boolean(document.querySelector('div.blame'));
}

function getLineEls(): Element[] {
  return Array.from(document.querySelectorAll(`.code-view td.lines-code${isBlame() ? '.blame-code' : ''}`));
}

function selectRange(linesEls: Element[], selectionEndEl: Element, selectionStartEl?: Element) {
  for (const el of linesEls) {
    el.closest('tr').classList.remove('active');
  }

  // add hashchange to permalink
  const refInNewIssue = document.querySelector('a.ref-in-new-issue');
  const copyPermalink = document.querySelector('a.copy-line-permalink');
  const viewGitBlame = document.querySelector('a.view_git_blame');

  const updateIssueHref = function (anchor) {
    if (!refInNewIssue) return;
    const urlIssueNew = refInNewIssue.getAttribute('data-url-issue-new');
    const urlParamBodyLink = refInNewIssue.getAttribute('data-url-param-body-link');
    const issueContent = `${toAbsoluteUrl(urlParamBodyLink)}#${anchor}`; // the default content for issue body
    refInNewIssue.setAttribute('href', `${urlIssueNew}?body=${encodeURIComponent(issueContent)}`);
  };

  const updateViewGitBlameFragment = function (anchor) {
    if (!viewGitBlame) return;
    let href = viewGitBlame.getAttribute('href');
    href = `${href.replace(/#L\d+$|#L\d+-L\d+$/, '')}`;
    if (anchor.length !== 0) {
      href = `${href}#${anchor}`;
    }
    viewGitBlame.setAttribute('href', href);
  };

  const updateCopyPermalinkUrl = function (anchor) {
    if (!copyPermalink) return;
    let link = copyPermalink.getAttribute('data-url');
    link = `${link.replace(/#L\d+$|#L\d+-L\d+$/, '')}#${anchor}`;
    copyPermalink.setAttribute('data-url', link);
  };

  if (selectionStartEl) {
    let a = parseInt(selectionEndEl.getAttribute('rel').slice(1));
    let b = parseInt(selectionStartEl.getAttribute('rel').slice(1));
    let c;
    if (a !== b) {
      if (a > b) {
        c = a;
        a = b;
        b = c;
      }
      const classes = [];
      for (let i = a; i <= b; i++) {
        classes.push(`[rel=L${i}]`);
      }
      for (const selectedLine of linesEls.filter((line) => line.matches(classes.join(',')))) {
        selectedLine.closest('tr').classList.add('active');
      }
      changeHash(`#L${a}-L${b}`);

      updateIssueHref(`L${a}-L${b}`);
      updateViewGitBlameFragment(`L${a}-L${b}`);
      updateCopyPermalinkUrl(`L${a}-L${b}`);
      return;
    }
  }
  selectionEndEl.closest('tr').classList.add('active');
  changeHash(`#${selectionEndEl.getAttribute('rel')}`);

  updateIssueHref(selectionEndEl.getAttribute('rel'));
  updateViewGitBlameFragment(selectionEndEl.getAttribute('rel'));
  updateCopyPermalinkUrl(selectionEndEl.getAttribute('rel'));
}

type LineTippy = {destroy: () => void, popper: HTMLElement, hide: () => void};
const lineTippies = new Set<LineTippy>();
let codeViewAttached = false;

function showLineButton() {
  const menu = document.querySelector('.code-line-menu');
  if (!menu) return;

  // remove all other line buttons
  for (const el of document.querySelectorAll('.code-line-button')) {
    el.remove();
  }
  for (const instance of lineTippies) instance.destroy();
  lineTippies.clear();

  // find active row and add button
  const tr = document.querySelector('.code-view tr.active');
  if (!tr) return;
  const td = tr.querySelector('td.lines-num');
  if (!td) return;
  const btn = document.createElement('button');
  btn.classList.add('code-line-button', 'ui', 'basic', 'button');
  btn.innerHTML = svg('octicon-kebab-horizontal');
  td.prepend(btn);

  // put a copy of the menu back into DOM for the next click
  btn.closest('.code-view')?.append(menu.cloneNode(true));

  const instance = createTippy(btn, {
    trigger: 'click',
    hideOnClick: true,
    content: menu,
    placement: 'right-start',
    interactive: true,
    onShow: (tippy) => {
      tippy.popper.addEventListener('click', () => {
        tippy.hide();
      }, {once: true});
    },
  });
  // createTippy returns a single instance or an array depending on tippy.js.
  for (const item of (Array.isArray(instance) ? instance : [instance])) lineTippies.add(item as unknown as LineTippy);
}

function onLineNumClick(e: Event) {
  const target = e.target as Element;
  if (target.closest(contributionRootSelector)) return;
  if (!target.matches('.lines-num span')) return;
  const linesEls = getLineEls();
  const selectedEl = linesEls.find((el) => el.matches(`[rel=${target.id}]`));
  if (!selectedEl) return;
  let from;
  if ((e as MouseEvent).shiftKey) {
    from = linesEls.find((el) => el.closest('tr')?.classList.contains('active'));
  }
  selectRange(linesEls, selectedEl, from);
  window.getSelection()?.removeAllRanges();
  showLineButton();
}

function onHashChange() {
  let m = window.location.hash.match(rangeAnchorRegex);
  const linesEls = getLineEls();
  let first;
  if (m) {
    first = linesEls.find((el) => el.matches(`[rel=L${m[1]}]`));
    if (first) {
      const last = linesEls.findLast((el) => el.matches(`[rel=L${m[2]}]`));
      selectRange(linesEls, first, last ?? linesEls.at(-1));
      if (!isBlame()) showLineButton();
      window.scrollBy({top: first.getBoundingClientRect().top - 200});
      return;
    }
  }
  m = window.location.hash.match(singleAnchorRegex);
  if (m) {
    first = linesEls.find((el) => el.matches(`[rel=L${m[1]}]`));
    if (first) {
      selectRange(linesEls, first);
      if (!isBlame()) showLineButton();
      window.scrollBy({top: first.getBoundingClientRect().top - 200});
    }
  }
}

function onFoldClick(e: Event) {
  const target = e.target as Element;
  if (target.closest(contributionRootSelector)) return;
  const foldFileButton = target.closest('.fold-file');
  if (!foldFileButton) return;
  const box = foldFileButton.closest('.file-content');
  if (box) invertFileFolding(box, foldFileButton);
}

async function onCopyPermalinkClick(e: Event) {
  const target = e.target as Element;
  if (target.closest(contributionRootSelector)) return;
  if (!target.matches('.copy-line-permalink')) return;
  await clippie(toAbsoluteUrl(target.getAttribute('data-url')));
}

export function initRepoCodeView() {
  if (codeViewAttached) return;
  codeViewAttached = true;
  if (document.querySelector('.code-view .lines-num')) {
    document.addEventListener('click', onLineNumClick);
    window.addEventListener('hashchange', onHashChange);
    window.dispatchEvent(new Event('hashchange'));
  }
  document.addEventListener('click', onFoldClick);
  document.addEventListener('click', onCopyPermalinkClick);
}

export function retireRepoCodeView() {
  document.removeEventListener('click', onLineNumClick);
  window.removeEventListener('hashchange', onHashChange);
  document.removeEventListener('click', onFoldClick);
  document.removeEventListener('click', onCopyPermalinkClick);
  for (const instance of lineTippies) {
    try { instance.destroy() } catch { /* already gone with the region */ }
  }
  lineTippies.clear();
  codeViewAttached = false;
}
