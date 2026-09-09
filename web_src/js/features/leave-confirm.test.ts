// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {test, describe, expect, beforeEach} from 'vitest';
import {_getValue, _onBeforeUnload, _storeOriginalValue, initGlobalFormDirtyLeaveConfirm, initLeaveConfirm} from './leave-confirm.ts';

beforeEach(() => {
  document.body.textContent = ''; // clear the DOM!
});

const textualCases = [
  // <input type="%[1]s" value="%[2]s">, later value="%[3]s"
  ['button', 'foo', 'bar'], // sure, i guess
  ['color', '#e66465', '#e66466'],
  ['date', '2018-07-22', '2018-07-23'],
  ['datetime-local', '2018-06-12T19:30', '2019-06-12T19:30'],
  ['email', 'test@forgejo.org', 'test@localhost'],
  ['file', '', ''], // the best we can do programmatically
  ['hidden', 'foo', 'bar'],
  ['month', '2018-05', '2019-05'],
  ['number', '42', '43'],
  ['password', 'foo', 'bar'],
  ['range', '8', '9'],
  ['reset', 'foo', 'bar'], // lol
  ['search', 'foo', 'bar'],
  ['submit', 'foo', 'bar'],
  ['tel', '+12125553151', '+12125553152'],
  ['text', 'foo', 'bar'],
  ['time', '14:00', '14:01'],
  ['url', 'https://forgejo.org/', 'https://codeberg.org/'],
  ['week', '2018-W18', '2018-W19'],
] as const;

/**
 * Creates an element that can have leave confirmation:
 * - has a `name` attribute
 * - is not "ignored"
 */
function createAcceptableElement<K extends keyof Pick<HTMLElementTagNameMap, 'input' | 'textarea' | 'select' | 'button'>>(tagName: K): HTMLElementTagNameMap[K] {
  const el = document.createElement(tagName);
  el.name = `${Math.random().toString(16)}000000000`.slice(2, 10);
  return el;
}

function dispatchChangeEvent(target: EventTarget) {
  target.dispatchEvent(new InputEvent('change', {bubbles: true}));
}

describe('onBeforeUnload', () => {
  let didPreventDefault: boolean;
  let fakeEvent: BeforeUnloadEvent;

  beforeEach(() => {
    didPreventDefault = false;
    fakeEvent = {
      preventDefault() {
        didPreventDefault = true;
      },
    } as unknown as BeforeUnloadEvent;
  });

  test('proceeds normally if no forms', () => {
    _onBeforeUnload(fakeEvent);
    expect(didPreventDefault).toBe(false);
  });

  test('proceeds normally if no dirty forms', () => {
    document.body.append(document.createElement('form'));
    _onBeforeUnload(fakeEvent);
    expect(didPreventDefault).toBe(false);
  });

  test('prevents default if a form is dirty', () => {
    const form = document.createElement('form');
    const i = createAcceptableElement('input');
    form.append(i);
    document.body.append(form);
    initLeaveConfirm(document.querySelectorAll('form'));
    i.value = 'foo';
    dispatchChangeEvent(i);
    expect(form.hasAttribute('data-dirty')).toBe(true);

    _onBeforeUnload(fakeEvent);
    expect(didPreventDefault).toBe(true);
  });
});

describe('dirtiness', () => {
  let form: HTMLFormElement;

  beforeEach(() => {
    form = document.createElement('form');
    for (const [type, value] of textualCases) {
      const el = createAcceptableElement('input');
      el.type = type;
      el.value = value;
      el.defaultValue = value;
      form.append(el);
    }

    const t = createAcceptableElement('textarea');
    t.name = 'textarea';
    t.value = 'foo';
    t.defaultValue = 'foo';
    form.append(t);

    const s = createAcceptableElement('select');
    const o1 = document.createElement('option');
    const o2 = document.createElement('option');
    const o3 = document.createElement('option');
    o1.value = 'foo';
    o2.value = 'bar';
    o3.value = 'baz';
    s.append(o1);
    s.append(o2);
    s.append(o3);
    form.append(s);

    const b = createAcceptableElement('button');
    b.type = 'submit';
    form.append(s);
    document.body.append(form);

    expect(document.body.childNodes.length).toBe(1); // just us chickens!
  });

  describe('init global', () => {
    test('initializes leave-confirm', () => {
      initGlobalFormDirtyLeaveConfirm();

      const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
      el.value = 'test@localhost';
      dispatchChangeEvent(el);
      expect(form.hasAttribute('data-dirty')).toBe(true); // 🎉
    });

    test('does nothing to an ignored form', () => {
      form.classList.add('ignore-dirty');
      initGlobalFormDirtyLeaveConfirm();

      const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
      el.value = 'test@localhost';
      dispatchChangeEvent(el);
      expect(form.hasAttribute('data-dirty')).toBe(false);
    });

    test('does nothing on a sign-in page', () => {
      const user = document.createElement('input');
      user.classList.add('user', 'signin');
      document.body.append(user);
      initGlobalFormDirtyLeaveConfirm();

      const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
      el.value = 'test@localhost';
      dispatchChangeEvent(el);
      expect(form.hasAttribute('data-dirty')).toBe(false);
    });
  });

  test('form is not initially dirty', () => {
    initLeaveConfirm(document.querySelectorAll('*'));
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('change event without any change does not dirty the form', () => {
    initLeaveConfirm(document.querySelectorAll('*'));
    const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    expect(form.hasAttribute('data-dirty')).toBe(false);
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  for (const [type, storedDefaultValue, newValue] of textualCases) {
    if (['button', 'file', 'submit'].includes(type)) {
      continue; // cannot programmatically set these ones
    }
    test(`changing one input with type ${type} dirties the form`, () => {
      initLeaveConfirm(document.querySelectorAll('*'));
      const el = form.querySelector(`input[type="${type}"]`) as HTMLInputElement;
      el.value = newValue;
      dispatchChangeEvent(el);
      expect(document.body.hasAttribute('data-dirty')).toBe(false); // non-form elements unaffected
      expect(el.hasAttribute('data-dirty')).toBe(false);
      expect(form.hasAttribute('data-dirty')).toBe(true); // only the form is marked "dirty"

      // restoring the value un-dirties the form 🧼
      if (['hidden', 'reset'].includes(type)) {
        // these don't keep a defaultValue
        el.value = storedDefaultValue;
      } else {
        el.value = el.defaultValue;
      }
      dispatchChangeEvent(el);
      expect(form.hasAttribute('data-dirty')).toBe(false);
    });
  }

  test('changing one textarea element dirties the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector('textarea');
    el.value = 'bar';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);

    el.value = el.defaultValue;
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing one select element dirties the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector('select');
    el.selectedIndex = 1;
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);

    el.selectedIndex = 0;
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing two elements and resetting one does not clear the "dirty" flag', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const e1 = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    const e2 = form.querySelector(`input[type="text"]`) as HTMLInputElement;
    e1.value = 'test@localhost';
    dispatchChangeEvent(e1);
    e2.value = 'bar';
    dispatchChangeEvent(e2);
    expect(form.hasAttribute('data-dirty')).toBe(true);

    e2.value = e2.defaultValue;
    dispatchChangeEvent(e2);
    expect(form.hasAttribute('data-dirty')).toBe(true);
  });

  test('disabling an element dirties the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    el.disabled = true;
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);

    el.disabled = false;
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing the value of a disabled field does not dirty the form', () => {
    const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    el.disabled = true;
    initLeaveConfirm(document.querySelectorAll('form'));
    expect(form.hasAttribute('data-dirty')).toBe(false);

    el.value = 'test@localhost';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing an "ignored" element does not dirty the form', () => {
    const ignored = createAcceptableElement('input');
    ignored.toggleAttribute('data-ignore-dirty', true); // TODO: We might consider applying this to forms on init as well, instead of an `ignore-dirty` class
    form.append(ignored);

    initLeaveConfirm(document.querySelectorAll('form'));
    expect(form.hasAttribute('data-dirty')).toBe(false);
    ignored.value = 'foo';
    dispatchChangeEvent(ignored);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing an "orphaned" element does not dirty the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector('input');
    el.remove();
    el.value = 'foo';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('changing a newly added input does not dirty the form by default', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = createAcceptableElement('input');
    form.append(el);

    el.value = 'foo';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);

    // not even after a re-check
    form.dispatchEvent(new Event('checkform-dirtiness'));
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('rescan allows a newly added input to dirty the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = createAcceptableElement('input');
    form.append(el);
    form.dispatchEvent(new Event('rescan-dirtiness')); // this tracks the current value as "original" so changes can mark the form dirty

    el.value = 'foo';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);
  });

  test('adding an input with [data-dirty] dirties the form after re-check', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = createAcceptableElement('input');
    el.type = 'hidden';
    el.toggleAttribute('data-dirty', true);

    form.append(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
    form.dispatchEvent(new Event('checkform-dirtiness')); // this works even without "rescan", the form checks all inputs and finds one marked "dirty"
    expect(form.hasAttribute('data-dirty')).toBe(true);

    el.remove();
    expect(form.hasAttribute('data-dirty')).toBe(true);
    form.dispatchEvent(new Event('checkform-dirtiness')); // changing another input would probably also work, we only need the form to re-check and notice the missing "dirty" input
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('adding an input with [data-dirty] dirties the form after re-scan', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = createAcceptableElement('input');
    el.type = 'hidden';
    el.toggleAttribute('data-dirty', true);

    form.append(el);
    expect(form.hasAttribute('data-dirty')).toBe(false);
    form.dispatchEvent(new Event('rescan-dirtiness')); // rescan implies recheck
    expect(form.hasAttribute('data-dirty')).toBe(true);

    el.remove();
    expect(form.hasAttribute('data-dirty')).toBe(true);
    form.dispatchEvent(new Event('rescan-dirtiness'));
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('re-scan does not cause a call loop with re-check', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    // weird state stuff is weird; these events shouldn't dispatch each other internally, but our own code may do so:
    form.addEventListener('checkform-dirtiness', () => {
      form.dispatchEvent(new Event('rescan-dirtiness'));
    });
    form.dispatchEvent(new Event('checkform-dirtiness'));
  });

  test('submit undirties the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    el.value = 'test@localhost';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);
    form.requestSubmit();
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });

  test('reset undirties the form', () => {
    initLeaveConfirm(document.querySelectorAll('form'));
    const el = form.querySelector(`input[type="email"]`) as HTMLInputElement;
    el.value = 'test@localhost';
    dispatchChangeEvent(el);
    expect(form.hasAttribute('data-dirty')).toBe(true);
    form.reset();
    expect(form.hasAttribute('data-dirty')).toBe(false);
  });
});

describe('get current input value', () => {
  test('returns null for non-sent form input elements (missing `name`)', () => {
    const el = document.createElement('input');
    el.value = 'foo';
    expect(_getValue(el)).toBeNull();
  });

  test('returns null for unsupported form input elements', () => {
    const el = document.createElement('div');
    el.setAttribute('name', 'foo');
    expect(_getValue(el as HTMLInputElement)).toBeNull();
  });

  test('returns null for ignored form input elements', () => {
    const el = createAcceptableElement('input');
    el.toggleAttribute('data-ignore-dirty', true);
    el.value = 'foo';
    expect(_getValue(el)).toBeNull();
  });

  test('returns null for disabled ignored form input elements', () => {
    const el = createAcceptableElement('input');
    el.toggleAttribute('data-ignore-dirty', true);
    el.value = 'foo';
    el.disabled = true;
    expect(_getValue(el)).toBeNull();
  });

  test('indicates that the form input element is disabled', () => {
    const el = createAcceptableElement('input');
    el.value = 'foo';
    el.disabled = true;
    expect(_getValue(el)).toEqual(expect.stringContaining('-disabled'));
  });

  test('value from default input', () => {
    const i = createAcceptableElement('input');
    i.value = 'foo';
    expect(_getValue(i)).toBe('foo');
  });

  for (const [type, value] of textualCases) {
    test(`value from ${type} input`, () => {
      const i = createAcceptableElement('input');
      i.type = type;
      i.value = value;
      expect(_getValue(i)).toBe(value);
    });
  }

  const checkableCases = [
    [false, 'false'],
    [true, 'true'],
  ] as const;
  for (const [checked, value] of checkableCases) {
    test(`value from checkbox (checked: ${checked})`, () => {
      const c = createAcceptableElement('input');
      c.type = 'checkbox';
      c.value = 'foo'; // this is ignored
      c.checked = checked;
      expect(_getValue(c)).toBe(value);
    });

    test(`value from radio button (checked: ${checked})`, () => {
      const r = createAcceptableElement('input');
      r.type = 'radio';
      r.value = 'foo'; // this is ignored
      r.checked = checked;
      expect(_getValue(r)).toBe(value);
    });
  }

  test('value from default button element', () => {
    const b = createAcceptableElement('button');
    b.value = 'foo';
    expect(_getValue(b)).toBe('foo');
  });

  const buttonTypes = ['button', 'reset', 'submit'] as const;
  for (const type of buttonTypes) {
    test(`value from button element (type: ${type})`, () => {
      const b = createAcceptableElement('button');
      b.type = type;
      b.value = 'foo';
      expect(_getValue(b)).toBe('foo');
    });
  }

  test('value from textarea element', () => {
    const t = createAcceptableElement('textarea');
    t.value = 'foo';
    expect(_getValue(t)).toBe('foo');
  });

  test('value from single select element', () => {
    const s = createAcceptableElement('select');
    const o1 = document.createElement('option');
    const o2 = document.createElement('option');
    const o3 = document.createElement('option');
    o1.value = 'foo';
    o2.value = 'bar';
    o3.value = 'baz';
    s.append(o1);
    s.append(o2);
    s.append(o3);

    s.selectedIndex = -1;
    expect(_getValue(s)).toBe('');

    s.selectedIndex = 0;
    expect(_getValue(s)).toBe('foo');

    s.selectedIndex = 1;
    expect(_getValue(s)).toBe('bar');

    o1.selected = true;
    expect(_getValue(s)).toBe('foo');
  });

  test('value from multi select element', () => {
    const s = createAcceptableElement('select');
    s.multiple = true;

    const o1 = document.createElement('option');
    const o2 = document.createElement('option');
    const o3 = document.createElement('option');
    o1.value = 'foo';
    o2.value = 'bar';
    o3.value = 'baz';
    s.append(o1);
    s.append(o2);
    s.append(o3);

    o1.selected = true;
    expect(_getValue(s)).toBe('foo');

    o2.selected = true;
    expect(_getValue(s)).toBe('foobar'); // the value here doesn't need to "mean" anything, only be comparable against the same input element later

    s.selectedIndex = -1;
    expect(_getValue(s)).toBe('');

    s.selectedIndex = 2;
    expect(_getValue(s)).toBe('baz');

    o1.selected = true;
    expect(_getValue(s)).toBe('foobaz');

    o2.selected = true;
    o3.selected = true;
    expect(_getValue(s)).toBe('foobarbaz');
  });
});

describe('store original', () => {
  test('does nothing to non-sent form input elements (missing `name`)', () => {
    const el = document.createElement('input');
    el.value = 'foo';
    _storeOriginalValue(el);
    expect(el.getAttribute('data-dirtiness-orig')).toBeNull();
  });

  test('does nothing to ignored form input elements', () => {
    const el = createAcceptableElement('input');
    el.toggleAttribute('data-ignore-dirty', true);
    el.value = 'foo';
    _storeOriginalValue(el);
    expect(el.getAttribute('data-dirtiness-orig')).toBeNull();
  });

  test('does nothing to disabled ignored form input elements', () => {
    const el = createAcceptableElement('input');
    el.toggleAttribute('data-ignore-dirty', true);
    el.value = 'foo';
    el.disabled = true;
    _storeOriginalValue(el);
    expect(el.getAttribute('data-dirtiness-orig')).toBeNull();
  });

  test('stores an indication that the form input element is disabled', () => {
    const el = createAcceptableElement('input');
    el.value = 'foo'; // this is ignored
    el.disabled = true;
    _storeOriginalValue(el);
    expect(el.getAttribute('data-dirtiness-orig')).toEqual(expect.stringContaining('-disabled')); // doesn't matter what the value is here, only that it compares differently against the same input element later if enabled
  });
});
