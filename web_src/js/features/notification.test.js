import {initNotificationsTable, retireNotificationsTable} from './notification.js';

function installTable() {
  document.body.innerHTML = `
    <span class="notifications-unread-count">2</span>
    <div id="notification_table">
      <div class="notifications-item" data-status="1"><a class="notifications-link">one</a></div>
      <div class="notifications-item" data-status="1"><a class="notifications-link">two</a></div>
    </div>`;
  return document.querySelectorAll('.notifications-item');
}

function pageShowPersisted() {
  const event = new Event('pageshow');
  event.persisted = true;
  window.dispatchEvent(event);
}

afterEach(() => {
  retireNotificationsTable();
  document.body.innerHTML = '';
});

test('clicked unread links are marked and removed once on bfcache restore', () => {
  const items = installTable();
  initNotificationsTable();
  initNotificationsTable();
  for (const link of document.querySelectorAll('.notifications-link')) link.click();
  expect(items[0].getAttribute('data-remove')).toBe('true');
  expect(items[1].getAttribute('data-remove')).toBe('true');
  pageShowPersisted();
  expect(document.querySelectorAll('.notifications-item')).toHaveLength(0);
  // the handler assigns the numeric 0, which happy-dom renders as ''
  expect(document.querySelector('.notifications-unread-count').textContent).toBe('');
});

test('bfcache restore decrements the unread count by the removed items', () => {
  installTable();
  initNotificationsTable();
  document.querySelector('.notifications-link').click();
  pageShowPersisted();
  expect(document.querySelectorAll('.notifications-item')).toHaveLength(1);
  expect(document.querySelector('.notifications-unread-count').textContent).toBe('1');
});

test('retire removes link and window listeners; re-init binds them again', () => {
  const items = installTable();
  initNotificationsTable();
  retireNotificationsTable();
  document.querySelector('.notifications-link').click();
  expect(items[0].hasAttribute('data-remove')).toBe(false);
  pageShowPersisted();
  expect(document.querySelectorAll('.notifications-item')).toHaveLength(2);
  initNotificationsTable();
  document.querySelector('.notifications-link').click();
  expect(items[0].getAttribute('data-remove')).toBe('true');
});

test('init without the table and retire without init are no-ops', () => {
  document.body.innerHTML = '';
  expect(() => initNotificationsTable()).not.toThrow();
  expect(() => retireNotificationsTable()).not.toThrow();
});
