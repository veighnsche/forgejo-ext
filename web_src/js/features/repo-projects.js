import $ from 'jquery';
import {contrastColor} from '../utils/color.js';
import {createSortable} from '../modules/sortable.js';
import {POST, DELETE, PUT} from '../modules/fetch.js';
import {contributionRootSelector} from '../native-lifecycle.js';

let projectState = null;

export function retireRepoProject() {
  const state = projectState;
  projectState = null;
  if (!state) return;
  state.controller.abort();
  for (const sortable of state.sortables) sortable.destroy();
  for (const el of state.bindings) $(el).off('.repoProject');
}

function updateIssueCount(cards) {
  const parent = cards.parentElement;
  const cnt = parent.getElementsByClassName('issue-card').length;
  parent.getElementsByClassName('project-column-issue-count')[0].textContent = cnt;
}

async function createNewColumn(url, columnTitle, projectColorInput, state) {
  try {
    await POST(url, {
      signal: state.controller.signal,
      data: {
        title: columnTitle.val(),
        color: projectColorInput.val(),
      },
    });
  } catch (error) {
    if (!state.controller.signal.aborted) console.error(error);
  } finally {
    if (!state.controller.signal.aborted && projectState === state) {
      columnTitle.closest('form').removeClass('dirty');
      window.location.reload();
    }
  }
}

async function moveIssue({item, from, to, oldIndex}, state) {
  const columnCards = to.getElementsByClassName('issue-card');
  updateIssueCount(from);
  updateIssueCount(to);

  const columnSorting = {
    issues: Array.from(columnCards, (card, i) => ({
      issueID: parseInt(card.getAttribute('data-issue')),
      sorting: i,
    })),
  };

  try {
    await POST(`${to.getAttribute('data-url')}/move`, {
      signal: state.controller.signal,
      data: columnSorting,
    });
  } catch (error) {
    if (!state.controller.signal.aborted && from.isConnected) {
      console.error(error);
      from.insertBefore(item, from.children[oldIndex]);
    }
  }
}

function initRepoProjectSortable(root, state) {
  const els = Array.from(root.querySelectorAll('#project-board > .board.sortable')).filter((el) => !el.closest(contributionRootSelector));
  if (!els.length) return;

  // the HTML layout is: #project-board > .board > .project-column .cards > .issue-card
  const mainBoard = els[0];
  let boardColumns = mainBoard.getElementsByClassName('project-column');
  const trackSortable = (el, options) => createSortable(el, options).then((sortable) => {
    if (projectState !== state || !el.isConnected) sortable.destroy();
    else state.sortables.push(sortable);
  });
  trackSortable(mainBoard, {
    group: 'project-column',
    draggable: '.project-column',
    handle: '.project-column-header',
    delayOnTouchOnly: true,
    delay: 500,
    onSort: async () => {
      boardColumns = mainBoard.getElementsByClassName('project-column');

      const columnSorting = {
        columns: Array.from(boardColumns, (column, i) => ({
          columnID: parseInt(column.getAttribute('data-id')),
          sorting: i,
        })),
      };

      try {
        await POST(mainBoard.getAttribute('data-url'), {
          signal: state.controller.signal,
          data: columnSorting,
        });
      } catch (error) {
        if (!state.controller.signal.aborted) console.error(error);
      }
    },
  });

  for (const boardColumn of boardColumns) {
    if (boardColumn.closest(contributionRootSelector)) continue;
    const boardCardList = boardColumn.getElementsByClassName('cards')[0];
    trackSortable(boardCardList, {
      group: 'shared',
      onAdd: (event) => moveIssue(event, state),
      onUpdate: (event) => moveIssue(event, state),
      delayOnTouchOnly: true,
      delay: 500,
    });
  }
}

export function initRepoProject() {
  retireRepoProject();
  const root = document.querySelector('.repository.projects');
  if (!root || root.closest(contributionRootSelector)) {
    return;
  }

  const state = {controller: new AbortController(), sortables: [], bindings: new Set()};
  projectState = state;
  const {signal} = state.controller;
  initRepoProjectSortable(root, state);

  for (const modal of root.getElementsByClassName('edit-project-column-modal')) {
    if (modal.closest(contributionRootSelector)) continue;
    const projectHeader = modal.closest('.project-column-header');
    const projectTitleLabel = projectHeader?.querySelector('.project-column-title-label');
    const projectTitleInput = modal.querySelector('.project-column-title-input');
    const projectColorInput = modal.querySelector('#new_project_column_color');
    const boardColumn = modal.closest('.project-column');
    modal.querySelector('.edit-project-column-button')?.addEventListener('click', async function (e) {
      e.preventDefault();
      try {
        await PUT(this.getAttribute('data-url'), {
          signal,
          data: {
            title: projectTitleInput?.value,
            color: projectColorInput?.value,
          },
        });
      } catch (error) {
        if (!signal.aborted) console.error(error);
      } finally {
        if (!signal.aborted && boardColumn.isConnected) {
          projectTitleLabel.textContent = projectTitleInput?.value;
          projectTitleInput.closest('form')?.classList.remove('dirty');
          const dividers = boardColumn.querySelectorAll(':scope > .divider');
          if (projectColorInput.value) {
            const color = contrastColor(projectColorInput.value);
            boardColumn.style.setProperty('background', projectColorInput.value, 'important');
            boardColumn.style.setProperty('color', color, 'important');
            for (const divider of dividers) {
              divider.style.setProperty('color', color);
            }
          } else {
            boardColumn.style.removeProperty('background');
            boardColumn.style.removeProperty('color');
            for (const divider of dividers) {
              divider.style.removeProperty('color');
            }
          }
          $(modal).modal('hide');
        }
      }
    }, {signal});
  }

  $(root).find('.default-project-column-modal').each(function () {
    if (this.closest(contributionRootSelector)) return;
    const $boardColumn = $(this).closest('.project-column');
    const $showButton = $($boardColumn).find('.default-project-column-show');
    const $commitButton = $(this).find('.actions > .ok.button');

    for (const el of $commitButton) state.bindings.add(el);
    $($commitButton).on('click.repoProject', async (e) => {
      e.preventDefault();

      try {
        await POST($($showButton).data('url'), {signal});
      } catch (error) {
        if (!signal.aborted) console.error(error);
      } finally {
        if (!signal.aborted) window.location.reload();
      }
    });
  });

  $(root).find('.show-delete-project-column-modal').each(function () {
    if (this.closest(contributionRootSelector)) return;
    const $deleteColumnModal = $(root).find(this.getAttribute('data-modal'));
    const $deleteColumnButton = $deleteColumnModal.find('.actions > .ok.button');
    const deleteUrl = this.getAttribute('data-url');

    for (const el of $deleteColumnButton) state.bindings.add(el);
    $deleteColumnButton.on('click.repoProject', async (e) => {
      e.preventDefault();

      try {
        await DELETE(deleteUrl, {signal});
      } catch (error) {
        if (!signal.aborted) console.error(error);
      } finally {
        if (!signal.aborted) window.location.reload();
      }
    });
  });

  const submit = root.querySelector('#new_project_column_submit');
  if (submit?.closest(contributionRootSelector)) return;
  if (submit) state.bindings.add(submit);
  $(submit).on('click.repoProject', (e) => {
    e.preventDefault();
    const $columnTitle = $(root).find('#new_project_column');
    const $projectColorInput = $(root).find('#new_project_column_color_picker');
    if (!$columnTitle.val()) {
      return;
    }
    const url = e.target.getAttribute('data-url');
    createNewColumn(url, $columnTitle, $projectColorInput, state);
  });
}
