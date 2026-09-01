// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

export function initGlobalFormDirtyLeaveConfirm() {
  // Warn users that try to leave a page after entering data into a form.
  // Except on sign-in pages, and for forms marked as 'ignore-dirty'.
  if (document.querySelector('.user.signin') === null) {
    initLeaveConfirm(document.querySelectorAll('form:not(.ignore-dirty)'));
  }
}

/**
 * A boolean attribute which indicates a "dirty" form.
 *
 * When `beforeunload` fires, if this attribute is found on any form in the
 * document, an alert is presented asking the user if they're sure they wish to
 * leave. This prevents accidentally leaving a filled form by e.g. refreshing
 * or closing the webpage.
 */
export const ATTR_DIRTY = 'data-dirty';
const SEL_DIRTY_FORM = `form[${ATTR_DIRTY}]`;

/**
 * A boolean attribute which causes the element to be ignored when checking the
 * form for "dirtiness".
 */
export const ATTR_IGNORE = 'data-ignore-dirty';

/**
 * leave-confirm dispatches an event of this type to watched forms when they
 * are found to no longer be "dirty".
 */
export const EVENT_CLEAN = 'form-clean';

/**
 * leave-confirm dispatches an event of this type to watched forms when they
 * are newly found to be "dirty".
 */
export const EVENT_DIRTY = 'form-dirty';

/**
 * leave-confirm dispatches an event of this type to watched forms when their
 * "dirty" state changes.
 */
export const EVENT_DIRTINESS_CHANGE = 'change-dirtiness';

/**
 * Dispatch this event to a watched form to instruct the form to begin watching
 * events from new input elements.
 *
 * Dispatching this event also re-checks the form for dirtiness.
 */
export const EVENT_RESCAN = 'rescan-dirtiness';

/**
 * Dispatch this event to a watched form to instruct the form to re-check for
 * "dirty" input elements.
 *
 * This is useful when adding or removing noninteractive input elements, such
 * as those with type `hidden`, to the form dynamically. When used in
 * combination with the `data-dirty` attribute on such elements, this event can
 * be used to manually force a form into a "dirty" state that won't be
 * overridden by changes to other input elements.
 */
export const EVENT_CHECKFORM = 'checkform-dirtiness';

const CHECK_EVENTS = ['change', 'keyup', 'input'] as const; // different from jquery-are-you-sure: omitting the IE-only 'propertychange' event
const ATTR_ORIG_VALUE = 'data-dirtiness-orig'; // we keep input elements' "original" value (as discovered on form init or rescan) in this attribute

/** Configuration options for a form watch session. All fields are required. */
export interface Settings {
  /**
   * An attribute value which indicates form "dirtiness". Should begin with
   * `data-`, but must not be empty or the value `data-dirty`.
   */
  dirtyAttr: string;

  /**
   * The selector with which to find applicable elements. Must not be empty.
   * Must match only `<input>`, `<textarea>`, `<select>`, or `<button>` elements.
   */
  inputSelector: string;
}

type InputElement = HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement | HTMLButtonElement;

/**
 * For each `<form>` element in `els`, begin watching for changes to input
 * values. If the user is about to leave the webpage with a "dirty" form (that
 * is, one that has been changed since last load or submit), warn them in the
 * standard way.
 *
 * `initLeaveConfirm` is intended to be called at least once to cover most or
 * all forms on the webpage. When calling `initLeaveConfirm` a second time, try
 * to scope the selector for `els` such that forms are not initialized multiple
 * times. This avoids excess memory use on the client.
 *
 * The interface is inspired by https://github.com/codedance/jquery.AreYouSure/:
 * - the `data-dirty` attribute on a form indicates "dirtiness" of its inputs
 * - manually setting `data-dirty` on a form *input* causes its parent form to
 *   perpetually read as "dirty"; useful for e.g. file uploaders with `hidden`
 *   inputs to represent new files
 * - a `beforeunload` handler warns about any "dirty" forms found on the page
 * - use `settings` to add additional "dirtiness" behavior for a given set of
 *   elements
 *
 * There are several important differences from the original:
 * - uses native CSS syntax for `settings.inputSelector`, such as
 *   `:where(input,textarea,select,button)` in place of jQuery's `:input`
 * - omits `addRemoveFieldsMarksDirty`; Forgejo sometimes adds `hidden` inputs
 *   to forms in such a way as to not imply form "dirtiness", so this function
 *   is often useless here. (We may add this behavior later if it is wanted.)
 * - only `change`, `keyup`, and `input` input events are watched, and custom
 *   event types are not currently supported
 * - drops support for IE and very-old Chromium
 *
 * @param els The list of `<form>` elements whose inputs to watch for changes.
 * @param options Configuration options for this form watch session.
 */
export function initLeaveConfirm(els: NodeListOf<Element>, settings?: Settings) {
  const dirtyAttr = settings?.dirtyAttr ?? ATTR_DIRTY;
  const inputSelector = settings?.inputSelector ?? ':where(input,textarea,select,button):not(input[type=submit]):not(input[type=button])';

  // standard unload listener...
  window.addEventListener('beforeunload', _onBeforeUnload); // Note: this only checks against ATTR_DIRTY, not settings.dirtyAttr!

  for (const el of els) {
    if (el.tagName !== 'FORM') {
      continue;
    }
    const form = el as HTMLFormElement;

    // watch for form-level events
    form.addEventListener('submit', onFormSubmit);
    form.addEventListener('reset', onFormReset);
    form.addEventListener(EVENT_RESCAN, rescan);
    form.addEventListener(EVENT_CHECKFORM, checkForm);

    // track current input values, and watch for further changes
    for (const input of form.querySelectorAll<InputElement>(inputSelector)) {
      _storeOriginalValue(input);
      for (const eventType of CHECK_EVENTS) {
        input.addEventListener(eventType, checkForm);
      }
      setFormDirty(form, false);
    }
  }

  function onFormSubmit(this: HTMLFormElement) {
    this.removeAttribute(dirtyAttr); // we're probably about to reload now; the user intentionally submitted, so there's no need to ask if they're sure
  }

  function onFormReset(this: HTMLFormElement) {
    setFormDirty(this, false); // not reloading, and values are back to defaults, so form no longer dirty!
  }

  /** Mark the form as dirty or not dirty, and dispatch appropriate events. */
  function setFormDirty(form: HTMLFormElement, isDirty: boolean) {
    const changed = isDirty !== form.hasAttribute(dirtyAttr);
    form.toggleAttribute(dirtyAttr, isDirty);

    if (changed) {
      if (isDirty) {
        form.dispatchEvent(new Event(EVENT_DIRTY, {bubbles: true}));
      } else {
        form.dispatchEvent(new Event(EVENT_CLEAN, {bubbles: true}));
      }
      form.dispatchEvent(new Event(EVENT_DIRTINESS_CHANGE, {bubbles: true}));
    }
  }

  /** Check the form or the input's parent form for dirty input elements. */
  function checkForm(this: HTMLFormElement | InputElement) {
    const form = this.tagName === 'FORM' ? this as HTMLFormElement : _formParent(this);
    if (!form) {
      // an input with no form 😢 this is an unlikely case, perhaps impossible!
      for (const eventName of CHECK_EVENTS) {
        this.removeEventListener(eventName, checkForm);
      }
      return;
    }

    // the target is most likely to be dirty (called from a change event)
    if (this.tagName !== 'FORM' && isInputDirty(this as InputElement)) {
      setFormDirty(form, true);
      return;
    }

    // target is clean (perhaps was reset) but its neighbors might not be
    for (const input of form.querySelectorAll<InputElement>(inputSelector)) {
      if (isInputDirty(input)) {
        setFormDirty(form, true);
        return;
      }
    }

    setFormDirty(form, false);
  }

  /**
   * Check the form for new input elements and begin watching them for future
   * changes. Also re-check the form for dirtiness.
   */
  function rescan(this: HTMLFormElement) {
    for (const input of this.querySelectorAll<InputElement>(inputSelector)) {
      if (!input.hasAttribute(ATTR_ORIG_VALUE)) {
        _storeOriginalValue(input);
        for (const eventName of CHECK_EVENTS) {
          input.addEventListener(eventName, checkForm);
        }
      }
    }
    checkForm.call(this); // we might dispatch checkform here instead, but I worry about creating loops in other places 😨
  }
}

/**
 * Returns `true` if the input element is "dirty", that is, it's either changed
 * since last we stored its value or it's been manually marked as such.
 */
function isInputDirty(el: InputElement): boolean {
  if (el.hasAttribute(ATTR_DIRTY)) {
    return true;
  }

  // This could simply compare el.value !== el.defaultValue, but this doesn't work for hidden inputs! (See https://forums.mozillazine.org/viewtopic.php?f=25&t=1787115)
  // Other input types may be similarly affected, or we may do things with our forms that invalidates defaultValue.
  // Comparing a version we store ourselves is safest until we can audit all our forms and ensure consistency.
  const og = el.getAttribute(ATTR_ORIG_VALUE);
  if (og === null) {
    return false; // null implies some invalid state or element, so we assume not dirty
  }
  return _getValue(el) !== og;
}

/** Returns the element's nearest parent `<form>` element, if any. */
export function _formParent(el: Element): HTMLFormElement | null {
  let parent = el.parentElement;
  if (parent?.tagName === 'FORM') {
    return parent as HTMLFormElement;
  }
  while (parent?.parentElement) {
    parent = parent.parentElement;
    if (parent?.tagName === 'FORM') {
      return parent as HTMLFormElement;
    }
  }
  return null;
}

/** Tracks the element's current value, if it's a value that we can manage. */
export function _storeOriginalValue(el: InputElement) {
  // jQuery used .data() for this, which stores the value internally. We use an element attr instead, so the value needs to be a string.
  const og = _getValue(el);
  if (og !== null) {
    el.setAttribute(ATTR_ORIG_VALUE, og);
  }
}

/**
 * Composes a string from the input value, or returns `null` if there is no
 * applicable element value.
 */
export function _getValue(el: InputElement): string | null {
  if (el.hasAttribute(ATTR_IGNORE) || !el.hasAttribute('name')) {
    return null;
  }

  if (el.disabled) {
    return '__forgejo-input-initially-disabled'; // real values probably won't ever match this
  }

  // Note: values must be distinguishable between values from the same input;
  // an empty string from a 'select' isn't comparable to that from a text
  // field, so there's no problem with returning '' for both, or "true" for a
  // boolean input, etc.
  switch (el.type) {
    case 'checkbox':
    case 'radio':
      // only <input type="{checkbox|radio}"> here:
      return (el as HTMLInputElement).checked.toString(); // "true" or "false"
    case 'select-multiple': {
      // The "value" of a <select multiple> is that of its selected options.
      // `el.value` is insufficient here, because that only represents the *first* selection!
      let value = '';
      for (const option of el.querySelectorAll('option')) {
        if (option.selected) {
          value += option.value;
        }
      }
      return value;
    }
    default:
      return el.value ?? null;
  }
}

/**
 * Searches the document for dirty forms. If any are found, a dialog is
 * presented to the user. Rejecting this dialog aborts the page unload.
 */
export function _onBeforeUnload(event: BeforeUnloadEvent): string {
  if (document.querySelector(SEL_DIRTY_FORM) === null) {
    return;
  }
  // Chrome and IE used to prompt multiple times, so some handling logic for that might've gone here.
  // But Forgejo doesn't support IE, and Chrome appears to have fixed this ages ago, so instead you find this comment!
  // We can add a fix later if someone notices that bug again.

  // trigger the dialog!!
  event.preventDefault();
  event.returnValue = true;
  return 'Are you sure? Your changes may be lost!'; // users won't ever see this string; https://chromestatus.com/feature/5349061406228480
}
