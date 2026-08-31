import {initCommonIssueListSelection, parseIssueListQuickGotoLink} from './common-issue-list.js';

test('parseIssueListQuickGotoLink', () => {
  expect(parseIssueListQuickGotoLink('/link', '')).toEqual('');
  expect(parseIssueListQuickGotoLink('/link', 'abc')).toEqual('');
  expect(parseIssueListQuickGotoLink('/link', '123')).toEqual('/link/issues/123');
  expect(parseIssueListQuickGotoLink('/link', '#123')).toEqual('/link/issues/123');
  expect(parseIssueListQuickGotoLink('/link', 'owner/repo#123')).toEqual('');

  expect(parseIssueListQuickGotoLink('', '')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'abc')).toEqual('');
  expect(parseIssueListQuickGotoLink('', '123')).toEqual('');
  expect(parseIssueListQuickGotoLink('', '#123')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'owner/repo#')).toEqual('');
  expect(parseIssueListQuickGotoLink('', 'owner/repo#123')).toEqual('/owner/repo/issues/123');
});

test('keeps repository filter and action toolbars in sync with partial selection', () => {
  document.body.innerHTML = '<div id="issue-filters"><div class="issue-list-toolbar-left"><input class="issue-checkbox-all" type="checkbox"></div></div>' +
    '<div id="issue-actions" class="tw-hidden"><div class="issue-list-toolbar-left"></div></div>' +
    '<div id="issue-list"><input class="issue-checkbox" type="checkbox"><input class="issue-checkbox" type="checkbox"></div>';
  initCommonIssueListSelection();
  const selectAll = document.querySelector('.issue-checkbox-all');
  const checkboxes = document.querySelectorAll('.issue-checkbox');
  checkboxes[0].click();
  expect(selectAll.indeterminate).toBe(true);
  expect(selectAll.closest('#issue-actions')).not.toBeNull();
  expect(document.querySelector('#issue-filters').classList.contains('tw-hidden')).toBe(true);
  selectAll.click();
  expect(Array.from(checkboxes).every((checkbox) => checkbox.checked)).toBe(true);
  selectAll.click();
  expect(Array.from(checkboxes).some((checkbox) => checkbox.checked)).toBe(false);
  expect(selectAll.closest('#issue-filters')).not.toBeNull();
});

test('selects only available dashboard rows without requiring repository toolbars', () => {
  document.body.innerHTML = '<input class="issue-checkbox-all" type="checkbox"><div id="issue-list">' +
    '<input class="issue-checkbox" type="checkbox"><input class="issue-checkbox" type="checkbox" disabled></div>';
  initCommonIssueListSelection();
  const selectAll = document.querySelector('.issue-checkbox-all');
  selectAll.click();
  expect(document.querySelector('.issue-checkbox:not(:disabled)').checked).toBe(true);
  expect(document.querySelector('.issue-checkbox:disabled').checked).toBe(false);
  expect(selectAll.checked).toBe(true);
  expect(selectAll.indeterminate).toBe(false);
});
