import $ from 'jquery';
import {updateIssuesMeta} from './repo-issue.js';
import {toggleElem, hideElem, isElemHidden} from '../utils/dom.js';
import {htmlEscape} from 'escape-goat';
import {confirmModal} from './comp/ConfirmModal.js';
import {showErrorToast} from '../modules/toast.js';
import {createSortable} from '../modules/sortable.js';
import {DELETE, POST} from '../modules/fetch.js';
import {parseDom} from '../utils.js';
import {contributionRootSelector} from '../native-lifecycle.js';

// Element listeners and widget instances owned by the current page. Retire
// removes exactly what init added; nodes inside retained contribution roots
// are never bound or destroyed here.
let bindings = [];
let authorDropdownEl = null;
let pinSortable = null;
let pinSortableWanted = false;

function bind(target, type, handler) {
  target.addEventListener(type, handler);
  bindings.push([target, type, handler]);
}

function isNative(el) {
  return el && !el.closest(contributionRootSelector);
}

function initRepoIssueListCheckboxes() {
  const issueSelectAll = document.querySelector('.issue-checkbox-all');
  if (!issueSelectAll || !isNative(issueSelectAll)) return; // logged out state
  const issueCheckboxes = Array.from(document.querySelectorAll('.issue-checkbox')).filter(isNative);

  const syncIssueSelectionState = () => {
    const checkedCheckboxes = issueCheckboxes.filter((el) => el.checked);
    const anyChecked = Boolean(checkedCheckboxes.length);
    const allChecked = anyChecked && checkedCheckboxes.length === issueCheckboxes.length;

    if (allChecked) {
      issueSelectAll.checked = true;
      issueSelectAll.indeterminate = false;
    } else if (anyChecked) {
      issueSelectAll.checked = false;
      issueSelectAll.indeterminate = true;
    } else {
      issueSelectAll.checked = false;
      issueSelectAll.indeterminate = false;
    }
    // if any issue is selected, show the action panel, otherwise show the filter panel
    toggleElem($('#issue-filters'), !anyChecked);
    toggleElem($('#issue-actions'), anyChecked);
    // there are two panels but only one select-all checkbox, so move the checkbox to the visible panel
    const panels = document.querySelectorAll('#issue-filters, #issue-actions');
    const visiblePanel = Array.from(panels).find((el) => !isElemHidden(el));
    const toolbarLeft = visiblePanel.querySelector('.issue-list-toolbar-left');
    toolbarLeft.prepend(issueSelectAll);
  };

  for (const el of issueCheckboxes) {
    bind(el, 'change', syncIssueSelectionState);
  }

  bind(issueSelectAll, 'change', () => {
    for (const el of issueCheckboxes) {
      el.checked = issueSelectAll.checked;
    }
    syncIssueSelectionState();
  });

  for (const el of document.querySelectorAll('.issue-action')) {
    if (!isNative(el)) continue;
    bind(el, 'click', async (e) => {
      e.preventDefault();

      const url = el.getAttribute('data-url');
      let action = el.getAttribute('data-action');
      let elementId = el.getAttribute('data-element-id');
      let issueIDs = [];
      for (const cb of document.querySelectorAll('.issue-checkbox:checked')) {
        issueIDs.push(cb.getAttribute('data-issue-id'));
      }
      issueIDs = issueIDs.join(',');
      if (!issueIDs) return;

      // for assignee
      if (elementId === '0' && url.endsWith('/assignee')) {
        elementId = '';
        action = 'clear';
      }

      // for toggle
      if (action === 'toggle' && e.altKey) {
        action = 'toggle-alt';
      }

      // for delete
      if (action === 'delete') {
        const confirmText = e.target.getAttribute('data-action-delete-confirm');
        if (!await confirmModal({content: confirmText, buttonColor: 'orange'})) {
          return;
        }
      }

      try {
        await updateIssuesMeta(url, action, issueIDs, elementId);
        window.location.reload();
      } catch (err) {
        showErrorToast(err.responseJSON?.error ?? err.message);
      }
    });
  }
}

function initRepoIssueListAuthorDropdown() {
  const $searchDropdown = $('.user-remote-search');
  if (!$searchDropdown.length || !isNative($searchDropdown[0])) return;
  authorDropdownEl = $searchDropdown[0];

  let searchUrl = $searchDropdown[0].getAttribute('data-search-url');
  const actionJumpUrl = $searchDropdown[0].getAttribute('data-action-jump-url');
  const selectedUserId = $searchDropdown[0].getAttribute('data-selected-user-id');
  if (!searchUrl.includes('?')) searchUrl += '?';

  $searchDropdown.dropdown('setting', {
    fullTextSearch: true,
    selectOnKeydown: false,
    apiSettings: {
      cache: false,
      url: `${searchUrl}&q={query}`,
      onResponse(resp) {
        // the content is provided by backend IssuePosters handler
        const processedResults = []; // to be used by dropdown to generate menu items
        for (const item of resp.results) {
          let html = `<img class="ui avatar tw-align-middle" src="${htmlEscape(item.avatar_link)}" aria-hidden="true" alt="" width="20" height="20"><span class="gt-ellipsis">${htmlEscape(item.username)}</span>`;
          if (item.full_name) html += `<span class="search-fullname tw-ml-2">${htmlEscape(item.full_name)}</span>`;
          processedResults.push({value: item.user_id, name: html});
        }
        resp.results = processedResults;
        return resp;
      },
    },
    action: (_text, value) => {
      window.location.href = actionJumpUrl.replace('{user_id}', encodeURIComponent(value));
    },
    onShow: () => {
      $searchDropdown.dropdown('filter', ' '); // trigger a search on first show
    },
  });

  // we want to generate the dropdown menu items by ourselves, replace its internal setup functions
  const dropdownSetup = {...$searchDropdown.dropdown('internal', 'setup')};
  const dropdownTemplates = $searchDropdown.dropdown('setting', 'templates');
  $searchDropdown.dropdown('internal', 'setup', dropdownSetup);
  dropdownSetup.menu = function (values) {
    const menu = $searchDropdown.find('> .menu')[0];
    // remove old dynamic items
    for (const el of menu.querySelectorAll(':scope > .dynamic-item')) {
      el.remove();
    }

    const newMenuHtml = dropdownTemplates.menu(values, $searchDropdown.dropdown('setting', 'fields'), true /* html */, $searchDropdown.dropdown('setting', 'className'));
    if (newMenuHtml) {
      const newMenuItems = parseDom(newMenuHtml, 'text/html').querySelectorAll('body > div');
      for (const newMenuItem of newMenuItems) {
        newMenuItem.classList.add('dynamic-item');
      }
      const div = document.createElement('div');
      div.classList.add('divider', 'dynamic-item');
      menu.append(div, ...newMenuItems);
    }
    $searchDropdown.dropdown('refresh');
    // defer our selection to the next tick, because dropdown will set the selection item after this `menu` function
    setTimeout(() => {
      for (const el of menu.querySelectorAll('.item.active, .item.selected')) {
        el.classList.remove('active', 'selected');
      }
      menu.querySelector(`.item[data-value="${selectedUserId}"]`)?.classList.add('selected');
    }, 0);
  };
}

function initPinRemoveButton() {
  for (const button of document.getElementsByClassName('issue-card-unpin')) {
    if (!isNative(button)) continue;
    bind(button, 'click', async (event) => {
      const el = event.currentTarget;
      const id = Number(el.getAttribute('data-issue-id'));

      // Send the unpin request
      const response = await DELETE(el.getAttribute('data-unpin-url'));
      if (response.ok) {
        // Delete the tooltip
        el._tippy.destroy();
        // Remove the Card
        el.closest(`div.issue-card[data-issue-id="${id}"]`).remove();
      }
    });
  }
}

async function pinMoveEnd(e) {
  const url = e.item.getAttribute('data-move-url');
  const id = Number(e.item.getAttribute('data-issue-id'));
  await POST(url, {data: {id, position: e.newIndex + 1}});
}

async function initIssuePinSort() {
  const pinDiv = document.getElementById('issue-pins');

  if (pinDiv === null || !isNative(pinDiv)) return;

  // If the User is not a Repo Admin, we don't need to proceed
  if (!pinDiv.hasAttribute('data-is-repo-admin')) return;

  initPinRemoveButton();

  // If only one issue pinned, we don't need to make this Sortable
  if (pinDiv.children.length < 2) return;

  pinSortableWanted = true;
  const instance = await createSortable(pinDiv, {
    group: 'shared',
    onEnd: pinMoveEnd,
  });
  // The page may have retired while the sortable chunk loaded.
  if (!pinSortableWanted) {
    instance.destroy();
    return;
  }
  pinSortable = instance;
}

function initArchivedLabelFilter() {
  const archivedLabelEl = document.querySelector('#archived-filter-checkbox');
  if (!archivedLabelEl || !isNative(archivedLabelEl)) {
    return;
  }

  const url = new URL(window.location.href);
  const archivedLabels = document.querySelectorAll('[data-is-archived]');

  if (!archivedLabels.length) {
    hideElem('.archived-label-filter');
    return;
  }
  const selectedLabels = (url.searchParams.get('labels') || '')
    .split(',')
    .map((id) => id < 0 ? `${~id + 1}` : id); // selectedLabels contains -ve ids, which are excluded so convert any -ve value id to +ve

  const archivedElToggle = () => {
    for (const label of archivedLabels) {
      const id = label.getAttribute('data-label-id');
      toggleElem(label.closest('.item'), archivedLabelEl.checked || selectedLabels.includes(id));
    }
  };

  archivedElToggle();

  bind(archivedLabelEl, 'change', () => {
    if (archivedLabelEl.checked) {
      url.searchParams.set('archived', 'true');
    } else {
      url.searchParams.delete('archived');
    }
    window.location.href = url.href;
  });
}

let listActive = false;

export function initRepoIssueList() {
  if (listActive) return;
  if (!document.querySelectorAll('.page-content.repository.issue-list, .page-content.repository.milestone-issue-list').length) return;
  listActive = true;
  initRepoIssueListCheckboxes();
  initRepoIssueListAuthorDropdown();
  initIssuePinSort();
  initArchivedLabelFilter();
}

export function retireRepoIssueList() {
  listActive = false;
  for (const [target, type, handler] of bindings) {
    target.removeEventListener(type, handler);
  }
  bindings = [];
  if (authorDropdownEl) {
    if (authorDropdownEl.isConnected) $(authorDropdownEl).dropdown('destroy');
    authorDropdownEl = null;
  }
  pinSortableWanted = false;
  pinSortable?.destroy();
  pinSortable = null;
}
