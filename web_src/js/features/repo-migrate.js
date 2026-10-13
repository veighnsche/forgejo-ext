import {hideElem, showElem} from '../utils/dom.js';
import {GET, POST} from '../modules/fetch.js';
import {contributionRootSelector} from '../native-lifecycle.js';

const {appSubUrl} = window.config;
let migrationController = null;
let migrationTimer;

export function retireRepoMigrationStatusChecker() {
  migrationController?.abort();
  migrationController = null;
  clearTimeout(migrationTimer);
  migrationTimer = undefined;
}

export function initRepoMigrationStatusChecker() {
  retireRepoMigrationStatusChecker();
  const repoMigrating = document.getElementById('repo_migrating');
  if (!repoMigrating || repoMigrating.closest(contributionRootSelector)) return;
  migrationController = new AbortController();
  const {signal} = migrationController;

  document.getElementById('repo_migrating_retry')?.addEventListener('click', async (e) => {
    await POST(e.currentTarget.getAttribute('data-migrating-task-retry-url'), {signal}).catch((error) => {
      if (!signal.aborted) throw error;
    });
    if (!signal.aborted && repoMigrating.isConnected) window.location.reload();
  }, {signal});

  const task = repoMigrating.getAttribute('data-migrating-task-id');

  // returns true if the refresh still needs to be called after a while
  const refresh = async () => {
    const res = await GET(`${appSubUrl}/user/task/${task}`, {signal});
    if (signal.aborted || !repoMigrating.isConnected) return false;
    if (res.url.endsWith('/login')) return false; // stop refreshing if redirected to login
    if (res.status !== 200) return true; // continue to refresh if network error occurs

    const data = await res.json();
    if (signal.aborted || !repoMigrating.isConnected) return false;

    // for all status
    if (data.message) {
      document.getElementById('repo_migrating_progress_message').textContent = data.message;
    }

    // TaskStatusFinished
    if (data.status === 4) {
      window.location.reload();
      return false;
    }

    // TaskStatusFailed
    if (data.status === 3) {
      hideElem('#repo_migrating_progress');
      hideElem('#repo_migrating');
      showElem('#repo_migrating_retry');
      showElem('#repo_migrating_failed');
      showElem('#repo_migrating_failed_image');
      document.getElementById('repo_migrating_failed_error').textContent = data.message;
      return false;
    }

    return true; // continue to refresh
  };

  const syncTaskStatus = async () => {
    let doNextRefresh = true;
    try {
      doNextRefresh = await refresh();
    } catch (error) {
      if (!signal.aborted) console.error(error);
    } finally {
      if (doNextRefresh && !signal.aborted && repoMigrating.isConnected) {
        migrationTimer = setTimeout(syncTaskStatus, 2000);
      }
    }
  };

  syncTaskStatus(); // no await
}
