import {createApp, type App} from 'vue';
import {contributionRootSelector, currentPageGeneration} from '../native-lifecycle.js';

type NativeMount = {element: HTMLElement; app?: App; parent?: HTMLDivElement; paddingBottom?: string};

let nativeMount: NativeMount | null = null;

export function retireRepositoryActionView() {
  const mount = nativeMount;
  nativeMount = null;
  mount?.app?.unmount();
  if (mount?.parent) mount.parent.style.paddingBottom = mount.paddingBottom;
}

export async function initRepositoryActionView() {
  const el = document.getElementById('repo-action-view');
  if (!el?.closest('.page-content') || el.closest(contributionRootSelector) || nativeMount?.element === el) return;

  retireRepositoryActionView();
  const mount: NativeMount = {element: el};
  nativeMount = mount;
  const generation = currentPageGeneration();

  try {
    const {default: RepoActionView} = await import(/* webpackChunkName: "repo-action-view" */'../components/RepoActionView.vue');
    if (nativeMount !== mount || generation !== currentPageGeneration() || !el.isConnected || document.getElementById('repo-action-view') !== el) {
      if (nativeMount === mount) retireRepositoryActionView();
      return;
    }

  // TODO: the parent element's full height doesn't work well now,
  // but we can not pollute the global style at the moment, only fix the height problem for pages with this component
    const parentFullHeight = document.querySelector('body > div.full.height') as HTMLDivElement;
    if (parentFullHeight) {
      mount.parent = parentFullHeight;
      mount.paddingBottom = parentFullHeight.style.paddingBottom;
      parentFullHeight.style.paddingBottom = '0';
    }

    const initialJobData = JSON.parse(el.getAttribute('data-initial-post-response'));
    const initialArtifactData = JSON.parse(el.getAttribute('data-initial-artifacts-response'));

    mount.app = createApp(RepoActionView, {
      initialJobData,
      initialArtifactData,
      runIndex: el.getAttribute('data-run-index'),
      runID: el.getAttribute('data-run-id'),
      jobIndex: el.getAttribute('data-job-index'),
      attemptNumber: el.getAttribute('data-attempt-number'),
      actionsURL: el.getAttribute('data-actions-url'),
      workflowName: el.getAttribute('data-workflow-name'),
      workflowURL: el.getAttribute('data-workflow-url'),
      workflowSourceURL: el.getAttribute('data-workflow-source-url'),
      locale: {
        approve: el.getAttribute('data-locale-approve'),
        cancel: el.getAttribute('data-locale-cancel'),
        rerun: el.getAttribute('data-locale-rerun'),
        artifactsTitle: el.getAttribute('data-locale-artifacts-title'),
        areYouSure: el.getAttribute('data-locale-are-you-sure'),
        confirmDeleteArtifact: el.getAttribute('data-locale-confirm-delete-artifact'),
        rerun_all: el.getAttribute('data-locale-rerun-all'),
        showTimeStamps: el.getAttribute('data-locale-show-timestamps'),
        showLogSeconds: el.getAttribute('data-locale-show-log-seconds'),
        showFullScreen: el.getAttribute('data-locale-show-full-screen'),
        downloadLogs: el.getAttribute('data-locale-download-logs'),
        runAttemptLabel: el.getAttribute('data-locale-run-attempt-label'),
        viewingOutOfDateRun: el.getAttribute('data-locale-viewing-out-of-date-run'),
        viewMostRecentRun: el.getAttribute('data-locale-view-most-recent-run'),
        preExecutionError: el.getAttribute('data-locale-pre-execution-error'),
        status: {
          unknown: el.getAttribute('data-locale-status-unknown'),
          waiting: el.getAttribute('data-locale-status-waiting'),
          running: el.getAttribute('data-locale-status-running'),
          success: el.getAttribute('data-locale-status-success'),
          failure: el.getAttribute('data-locale-status-failure'),
          cancelled: el.getAttribute('data-locale-status-cancelled'),
          skipped: el.getAttribute('data-locale-status-skipped'),
          blocked: el.getAttribute('data-locale-status-blocked'),
        },
      },
    });
    mount.app.mount(el);
  } catch (error) {
    if (nativeMount !== mount) return;
    retireRepositoryActionView();
    throw error;
  }
}
