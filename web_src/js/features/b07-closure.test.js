import $ from 'jquery';
import {vi} from 'vitest';
import {readFileSync} from 'node:fs';
import {initCompSearchUserBox, retireCompSearchUserBox} from './comp/SearchUserBox.js';
import {initCompWebHookEditor, retireCompWebHookEditor} from './comp/WebHookEditor.js';
import {initCommonOrganization, retireCommonOrganization} from './common-organization.js';

const index = readFileSync('web_src/js/index.js', 'utf8');

test('B07 wires B04 page features and conversation-nav', () => {
  for (const name of ['repo-code-view', 'repo-editor', 'repo-diff-view', 'repo-diff-conversation-nav', 'repo-wiki-form', 'repo-graph', 'repo-find-file', 'repo-image-diff', 'repo-diff-commit-branches-and-tags']) {
    expect(index).toContain(`registerPageFeature('${name}'`);
  }
});

test('B07 wires B05 19 page features', () => {
  for (const name of ['repo-issue-list', 'common-issue-list-quick-goto', 'repo-issue-content-history', 'repo-issue-sidebar-list', 'repo-comment-form', 'repo-pull-request-update', 'repo-pull-request-allow-maintainer-edit', 'repo-issue-reference-repository-search', 'repo-issue-wip-title', 'repo-issue-comments', 'repo-pull-request-review', 'repo-issue-wip-toggle', 'repo-issue-title-edit', 'repo-issue-branch-select', 'repo-issue-assign-me', 'repo-pull-request-merge-form', 'repo-pull-request-commit-status', 'repo-settings-collaboration', 'archived-label-handler']) {
    expect(index).toContain(`registerPageFeature('${name}'`);
  }
});

test('B07 wires B06 7 page features via common-organization for LabelEdit', () => {
  for (const name of ['admin-common', 'admin-configs', 'admin-emails', 'common-organization', 'notifications-table', 'org-team-search-repo-box', 'ssh-key-form-parser']) {
    expect(index).toContain(`registerPageFeature('${name}'`);
  }
  expect(index).not.toContain(`registerPageFeature('comp-label-edit'`);
  expect(index).toContain('retireCommonOrganization');
});

test('B07 wires document-lifetime globals and notification worker once', () => {
  for (const name of ['repo-issue-time-tracking', 'repo-issue-due', 'repo-issue-comment-delete', 'repo-issue-dependency-delete', 'repo-issue-code-comment-cancel', 'repo-issue-reference-issue', 'notification-count']) {
    expect(index).toContain(`registerGlobalFeature('${name}'`);
  }
});

test('B07 removes bare page calls for converted features', () => {
  for (const direct of ['initCommonOrganization();', 'initCommonIssueListQuickGoto();', 'initCompSearchUserBox();', 'initCompWebHookEditor();', 'initImageDiff();', 'initSshKeyFormParser();', 'initFindFileInRepo();', 'initAdminCommon();', 'initAdminEmails();', 'initAdminConfigs();', 'initNotificationsTable();', 'initNotificationCount();', 'initOrgTeamSearchRepoBox();', 'initRepoCodeView();', 'initRepoCommentForm();', 'initRepoDiffCommitBranchesAndTags();', 'initRepoEditor();', 'initRepoGraphGit();', 'initRepoIssueContentHistory();', 'initRepoIssueDue();', 'initRepoIssueList();', 'initRepoIssueSidebarList();', 'initRepoIssueReferenceRepositorySearch();', 'initRepoIssueTimeTracking();', 'initRepoIssueWipTitle();', 'initRepoPullRequestAllowMaintainerEdit();', 'initRepoPullRequestReview();', 'initRepoSettingsCollaboration();', 'initRepoWikiForm();', 'initRepoDiffView();']) {
    expect(index).not.toContain(`\n  ${direct}`);
  }
  expect(index).toContain('initPageFeatures();');
  expect(index).toContain('initGlobalFeatures();');
});

test('B07 handles comp ownership and the page-owned dashboard', () => {
  expect(index).toContain(`registerPageFeature('comp-search-user-box'`);
  expect(index).toContain(`registerPageFeature('comp-webhook-editor'`);
  expect(index).toContain("registerPageFeature('dashboard-repo-list', {init: initDashboardRepoList, retire: retireDashboardRepoList})");
  expect(index).not.toContain('\n  initDashboardRepoList();');
});

test('B07 registers the remaining native repository widgets with their actual retirement hooks', () => {
  const features = [
    ['repo-branch-button', 'initRepoBranchButton', 'retireRepoBranchButton'],
    ['repo-branch-tag-selector', 'initRepoBranchTagSelector', 'retireRepoBranchTagSelector'],
    ['repo-ellipsis', 'initRepoEllipsisButton', 'retireRepoEllipsisButton'],
    ['repo-migration', 'initRepoMigration', 'retireRepoMigration'],
    ['repo-migration-status', 'initRepoMigrationStatusChecker', 'retireRepoMigrationStatusChecker'],
    ['repo-project', 'initRepoProject', 'retireRepoProject'],
    ['repo-release', 'initRepoRelease', 'retireRepoRelease'],
    ['repo-release-new', 'initRepoReleaseNew', 'retireRepoReleaseNew'],
    ['repo-settings-git-hook', 'initRepoSettingGitHook', 'retireRepoSettingGitHook'],
    ['repo-settings-team-search', 'initRepoSettingSearchTeamBox', 'retireRepoSettingSearchTeamBox'],
    ['repo-settings-branches', 'initRepoSettingBranches', 'retireRepoSettingBranches'],
    ['repo-template-search', 'initRepoTemplateSearch', 'retireRepoTemplateSearch'],
    ['repo-topic-bar', 'initRepoTopicBar', 'retireRepoTopicBar'],
    ['repository', 'initRepository', 'retireRepository'],
    ['repo-search', 'initRepositorySearch', 'retireRepositorySearch'],
    ['repo-milestone-editor', 'initRepoMilestoneEditor', 'retireRepoMilestoneEditor'],
    ['commit-statuses', 'initCommitStatuses', 'retireCommitStatuses'],
    ['commit-notes', 'initCommitNotes', 'retireCommitNotes'],
    ['repo-activity-top-authors', 'initRepoActivityTopAuthorsChart', 'retireRepoActivityTopAuthorsChart'],
    ['repo-action-view', 'initRepositoryActionView', 'retireRepositoryActionView'],
    ['repo-contributors', 'initRepoContributors', 'retireRepoContributors'],
    ['repo-code-frequency', 'initRepoCodeFrequency', 'retireRepoCodeFrequency'],
    ['repo-recent-commits', 'initRepoRecentCommits', 'retireRepoRecentCommits'],
  ];
  for (const [name, init, retire] of features) {
    expect(index).toContain(`registerPageFeature('${name}', {init: ${init}, retire: ${retire}})`);
    expect(index).not.toContain(`\n  ${init}();`);
  }
  expect(index).toContain("registerPageFeature('repo-common', {");
  expect(index).toContain('retire: retireRepoCommon');
  expect(index).not.toContain('\n  initRepoArchiveLinks();');
  expect(index).not.toContain('\n  initRepoCloneLink();');
  expect(index).toContain("registerGlobalFeature('stopwatch', initStopwatch)");
  expect(index).toContain("registerGlobalFeature('repo-unicode-escape', initUnicodeEscapeButton)");
  expect(index).toContain("registerGlobalFeature('repo-actions-list', initRepoActionsList)");
});

test('native boot reads declarative page data before initial page features', () => {
  const boot = index.slice(index.indexOf('onDomReady(() => {'));
  expect(boot).toContain('const pageData = readNativePageData(document);');
  expect(boot.indexOf('if (pageData) setPageConfig(pageData);')).toBeLessThan(boot.indexOf('initGlobalFeatures();'));
  expect(boot.indexOf('if (pageData) setPageConfig(pageData);')).toBeLessThan(boot.indexOf('initPageFeatures();'));
});

test('SearchUserBox init is idempotent and retire destroys the widget', () => {
  const calls = [];
  $.fn.search = vi.fn(function (...args) { calls.push(args); return this });
  try {
    document.body.innerHTML = `<div id="search-user-box" data-allow-email="false"><input></div>`;
    initCompSearchUserBox();
    initCompSearchUserBox();
    expect(calls.filter((c) => typeof c[0] === 'object')).toHaveLength(1);
    retireCompSearchUserBox();
    expect(calls).toContainEqual(['destroy']);
    initCompSearchUserBox();
    expect(calls.filter((c) => typeof c[0] === 'object')).toHaveLength(2);
  } finally {
    retireCompSearchUserBox();
    delete $.fn.search;
    document.body.innerHTML = '';
  }
});

test('WebHookEditor retire removes listeners and re-init rebinds once', () => {
  document.body.innerHTML = `<div class="new webhook"><div class="field"><input id="http_method" value="GET"><div class="field"><span id="content_type"></span></div></div><button id="test-delivery" data-link="/x" data-redirect="/y"></button></div>`;
  initCompWebHookEditor();
  initCompWebHookEditor();
  const input = document.getElementById('http_method');
  input.value = 'POST';
  input.dispatchEvent(new Event('change'));
  retireCompWebHookEditor();
  input.dispatchEvent(new Event('change'));
  expect(() => retireCompWebHookEditor()).not.toThrow();
  document.body.innerHTML = '';
});

test('repeated organization entry does not leak label listeners', () => {
  document.body.innerHTML = `<div class="organization settings options"><input id="org_name" data-org-name="old"><div id="org-name-change-prompt" class="tw-hidden"></div></div><div class="organization settings labels"><button class="new-label button"></button></div>`;
  initCommonOrganization();
  initCommonOrganization();
  const input = document.getElementById('org_name');
  input.value = 'new';
  input.dispatchEvent(new Event('input'));
  expect(document.getElementById('org-name-change-prompt').classList.contains('tw-hidden')).toBe(false);
  retireCommonOrganization();
  input.value = 'old';
  input.dispatchEvent(new Event('input'));
  // after retire the prompt stays visible (handler removed)
  expect(document.getElementById('org-name-change-prompt').classList.contains('tw-hidden')).toBe(false);
  retireCommonOrganization();
  document.body.innerHTML = '';
});
