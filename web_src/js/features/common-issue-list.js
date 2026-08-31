import {isElemHidden, onInputDebounce, submitEventSubmitter, toggleElem} from '../utils/dom.js';
import {GET} from '../modules/fetch.js';

const {appSubUrl} = window.config;
const reIssueIndex = /^(\d+)$/; // eg: "123"
const reIssueSharpIndex = /^#(\d+)$/; // eg: "#123"
const reIssueOwnerRepoIndex = /^([-.\w]+)\/([-.\w]+)#(\d+)$/;  // eg: "{owner}/{repo}#{index}"

// Repository metadata actions and dashboard merges share the same selection.
export function initCommonIssueListSelection() {
  const issueList = document.querySelector('#issue-list');
  const selectAll = document.querySelector('.issue-checkbox-all');
  if (!issueList || !selectAll) return;
  const checkboxes = Array.from(issueList.querySelectorAll('.issue-checkbox'));
  const filters = document.querySelector('#issue-filters');
  const actions = document.querySelector('#issue-actions');

  const sync = () => {
    const available = checkboxes.filter((el) => !el.disabled);
    const selected = available.filter((el) => el.checked);
    selectAll.checked = selected.length > 0 && selected.length === available.length;
    selectAll.indeterminate = selected.length > 0 && selected.length < available.length;
    selectAll.disabled = available.length === 0;
    if (filters && actions) {
      toggleElem(filters, selected.length === 0);
      toggleElem(actions, selected.length > 0);
      const panel = selected.length > 0 ? actions : filters;
      panel.querySelector('.issue-list-toolbar-left').prepend(selectAll);
    }
    issueList.dispatchEvent(new CustomEvent('issue-selection-change'));
  };
  for (const checkbox of checkboxes) {
    checkbox.addEventListener('change', sync);
  }
  selectAll.addEventListener('change', () => {
    for (const checkbox of checkboxes) {
      if (!checkbox.disabled) checkbox.checked = selectAll.checked;
    }
    sync();
  });
  sync();
}

// if the searchText can be parsed to an "issue goto link", return the link, otherwise return empty string
export function parseIssueListQuickGotoLink(repoLink, searchText) {
  searchText = searchText.trim();
  let targetUrl = '';
  if (repoLink) {
    // try to parse it in current repo
    if (reIssueIndex.test(searchText)) {
      targetUrl = `${repoLink}/issues/${searchText}`;
    } else if (reIssueSharpIndex.test(searchText)) {
      targetUrl = `${repoLink}/issues/${searchText.substr(1)}`;
    }
  } else {
    // try to parse it for a global search (eg: "owner/repo#123")
    const matchIssueOwnerRepoIndex = searchText.match(reIssueOwnerRepoIndex);
    if (matchIssueOwnerRepoIndex) {
      const [_, owner, repo, index] = matchIssueOwnerRepoIndex;
      targetUrl = `${appSubUrl}/${owner}/${repo}/issues/${index}`;
    }
  }
  return targetUrl;
}

export function initCommonIssueListQuickGoto() {
  const goto = document.getElementById('issue-list-quick-goto');
  if (!goto) return;

  const form = goto.closest('form');
  const input = form.querySelector('input[name=q]');
  const repoLink = goto.getAttribute('data-repo-link');

  form.addEventListener('submit', (e) => {
    // if there is no goto button, or the form is submitted by non-quick-goto elements, submit the form directly
    let doQuickGoto = !isElemHidden(goto);
    const submitter = submitEventSubmitter(e);
    if (submitter !== form && submitter !== input && submitter !== goto) doQuickGoto = false;
    if (!doQuickGoto) return;

    // if there is a goto button, use its link
    e.preventDefault();
    window.location.href = goto.getAttribute('data-issue-goto-link');
  });

  const onInput = async () => {
    const searchText = input.value;
    // try to check whether the parsed goto link is valid
    let targetUrl = parseIssueListQuickGotoLink(repoLink, searchText);
    if (targetUrl) {
      const res = await GET(`${targetUrl}/info`);
      if (res.status !== 200) targetUrl = '';
    }
    // if the input value has changed, then ignore the result
    if (input.value !== searchText) return;

    toggleElem(goto, Boolean(targetUrl));
    goto.setAttribute('data-issue-goto-link', targetUrl);
  };

  input.addEventListener('input', onInputDebounce(onInput));
  onInput();
}
