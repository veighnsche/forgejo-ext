import {initRepoPullRequestMergeForm, retireRepoPullRequestMergeForm} from './repo-issue-pr-form.js';

const {apps} = vi.hoisted(() => ({apps: []}));

vi.mock('vue', () => ({
  createApp: () => {
    const app = {mount: vi.fn(), unmount: vi.fn()};
    apps.push(app);
    return app;
  },
}));
vi.mock('../components/PullRequestMergeForm.vue', () => ({default: {name: 'PullRequestMergeForm'}}));

afterEach(() => {
  retireRepoPullRequestMergeForm();
  apps.length = 0;
  document.body.innerHTML = '';
});

test('merge form mounts once and unmounts on retire', async () => {
  document.body.innerHTML = `<div id="pull-request-merge-form"></div>`;
  const el = document.getElementById('pull-request-merge-form');
  await initRepoPullRequestMergeForm();
  await initRepoPullRequestMergeForm();
  expect(apps).toHaveLength(1);
  expect(apps[0].mount).toHaveBeenCalledWith(el);
  retireRepoPullRequestMergeForm();
  expect(apps[0].unmount).toHaveBeenCalledTimes(1);
  // A fresh page mounts a fresh app after retirement.
  document.body.innerHTML = `<div id="pull-request-merge-form"></div>`;
  await initRepoPullRequestMergeForm();
  expect(apps).toHaveLength(2);
});

test('no merge form element means no app', async () => {
  await initRepoPullRequestMergeForm();
  expect(apps).toHaveLength(0);
});
