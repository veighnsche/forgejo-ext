import $ from 'jquery';
import {initRepoIssueSidebarList, retireRepoIssueSidebarList} from './repo-issue-sidebar-list.ts';

// The shared setup stubs $.fn.dropdown as a non-chainable no-op; fomantic
// returns the jQuery object, which the sidebar init chains on.
$.fn.dropdown = function () { return this; };

function installSidebar() {
  document.body.innerHTML = `<input type="hidden" id="repolink" value="owner/repo">
  <input type="hidden" id="repoId" value="1">
  <input type="hidden" id="crossRepoSearch" value="false">
  <input type="hidden" id="type" value="issues">
  <div id="new-dependency-drop-list"></div>
  <div class="menu">
    <div class="item">
      <a class="label-filter-item" data-label-id="3">bug</a>
      <button class="label-exclude-item-btn" type="button">x</button>
    </div>
  </div>
  <div class="ui dropdown label-filter"></div>`;
}

afterEach(() => {
  retireRepoIssueSidebarList();
  document.body.innerHTML = '';
  window.history.replaceState(null, '', '/');
});

test('label exclusion binds once through the sidebar lifecycle', () => {
  installSidebar();
  initRepoIssueSidebarList();
  initRepoIssueSidebarList();
  document.querySelector('.label-exclude-item-btn').click();
  // One binding excludes once; a stacked second handler would toggle it off.
  expect(new URLSearchParams(window.location.search).get('labels')).toBe('-3');
});

test('retire detaches the label exclusion handler', () => {
  installSidebar();
  initRepoIssueSidebarList();
  retireRepoIssueSidebarList();
  document.querySelector('.label-exclude-item-btn').click();
  expect(new URLSearchParams(window.location.search).get('labels')).toBeNull();
});
